package serveraclinit

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/consul/api"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

// bootstrapServers bootstraps ACLs and ensures each server has an ACL token.
// If bootstrapToken is not empty then ACLs are already bootstrapped.
func (c *Command) bootstrapServers(serverAddresses []net.IPAddr, bootstrapToken, bootTokenSecretName string) (string, error) {
	// Pick the first server address to connect to for bootstrapping and set up connection.
	firstServerAddr := fmt.Sprintf("%s:%d", serverAddresses[0].IP.String(), c.consulFlags.HTTPPort)

	if bootstrapToken == "" {
		c.log.Info("No bootstrap token from previous installation found, continuing on to bootstrapping")

		var err error
		bootstrapToken, err = c.bootstrapACLs(firstServerAddr, bootTokenSecretName)
		if err != nil {
			return "", err
		}
	} else {
		c.log.Info(fmt.Sprintf("ACLs already bootstrapped - retrieved bootstrap token from Secret %q", bootTokenSecretName))
	}

	// We should only create and set server tokens when servers are running within this cluster.
	if c.flagSetServerTokens {
		c.log.Info("Setting Consul server tokens")
		// Create new tokens for each server and apply them.
		if err := c.setServerTokens(serverAddresses, bootstrapToken); err != nil {
			return "", err
		}
	}
	return bootstrapToken, nil
}

// bootstrapACLs makes the ACL bootstrap API call and writes the bootstrap token
// to a kube secret.
func (c *Command) bootstrapACLs(firstServerAddr, bootTokenSecretName string) (string, error) {
	config := c.consulFlags.ConsulClientConfig().APIClientConfig
	config.Address = firstServerAddr
	// Exempting this particular use of the http client from using global.consulAPITimeout
	// which defaults to 5 seconds.  In acceptance tests, we saw that the call
	// to /v1/acl/bootstrap taking 5-7 seconds and when it does, the request times
	// out without returning the bootstrap token, but the bootstrapping does complete.
	// This would leave cases where server-acl-init job would get a 403 that it had
	// already bootstrapped and would not be able to complete.
	// Since this is an area where we have to wait and can't retry, we are setting it
	// to a large number like 5 minutes since previously this had no timeout.
	config.HttpClient = &http.Client{Timeout: 5 * time.Minute}
	consulClient, err := consul.NewClient(config, c.consulFlags.APITimeout)

	if err != nil {
		return "", fmt.Errorf("creating Consul client for address %s: %s", firstServerAddr, err)
	}

	// Call bootstrap ACLs API.
	var bootstrapToken string
	var unrecoverableErr error
	err = c.untilSucceeds("bootstrapping ACLs - PUT /v1/acl/bootstrap",
		func() error {
			bootstrapResp, _, err := consulClient.ACL().Bootstrap()
			if err == nil {
				bootstrapToken = bootstrapResp.SecretID
				return nil
			}

			// Check if already bootstrapped.
			if strings.Contains(err.Error(), "Unexpected response code: 403") {
				unrecoverableErr = errors.New("ACLs already bootstrapped but the ACL token was not written to a Kubernetes secret." +
					" We can't proceed because the bootstrap token is lost." +
					" You must reset ACLs.")
				return nil
			}

			// Check if we need to re-attempt DNS lookup.
			if strings.Contains(err.Error(), "connection refused") {
				unrecoverableErr = errors.New("Cannot reach consul server via DNS, restarting")
				return nil
			}

			if isNoLeaderErr(err) {
				// Return a more descriptive error in the case of no leader
				// being elected.
				return fmt.Errorf("no leader elected: %s", err)
			}
			return err
		})
	if unrecoverableErr != nil {
		return "", unrecoverableErr
	}
	if err != nil {
		return "", err
	}

	// Write bootstrap token to a Kubernetes secret.
	err = c.untilSucceeds(fmt.Sprintf("writing bootstrap Secret %q", bootTokenSecretName),
		func() error {
			secret := &apiv1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   bootTokenSecretName,
					Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
				},
				Data: map[string][]byte{
					common.ACLTokenSecretKey: []byte(bootstrapToken),
				},
			}
			_, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Create(c.ctx, secret, metav1.CreateOptions{})
			return err
		})
	return bootstrapToken, err
}

// setServerTokens creates policies and associated ACL token for each server
// and then provides the token to the server.
func (c *Command) setServerTokens(serverAddresses []net.IPAddr, bootstrapToken string) error {
	// server specifically.
	clientConfig := c.consulFlags.ConsulClientConfig().APIClientConfig
	clientConfig.Address = fmt.Sprintf("%s:%d", serverAddresses[0].IP.String(), c.consulFlags.HTTPPort)
	clientConfig.Token = bootstrapToken
	serverClient, err := consul.NewClient(clientConfig,
		c.consulFlags.APITimeout)
	if err != nil {
		return err
	}
	agentPolicy, err := c.setServerPolicy(serverClient)
	if err != nil {
		return err
	}

	existingTokens, _, err := serverClient.ACL().TokenList(nil)
	if err != nil {
		return err
	}

	// Create agent token for each server agent.
	for _, host := range serverAddresses {
		var tokenSecretID string

		// We create a new client for each server because we need to call each
		// server specifically.
		clientConfig := c.consulFlags.ConsulClientConfig().APIClientConfig
		clientConfig.Address = fmt.Sprintf("%s:%d", host.IP.String(), c.consulFlags.HTTPPort)
		clientConfig.Token = bootstrapToken
		serverClient, err := consul.NewClient(clientConfig,
			c.consulFlags.APITimeout)
		if err != nil {
			return err
		}

		tokenDescription := fmt.Sprintf("Server Token for %s", host.IP.String())

		// Check if the token was already created. We're matching on the description
		// since that's the only part that's unique.
		for _, t := range existingTokens {
			if len(t.Policies) == 1 && t.Policies[0].Name == agentPolicy.Name {
				if t.Description == tokenDescription {
					tokenSecretID = t.SecretID
					break
				}
			}
		}

		// Create token for the server if it doesn't already exist.
		if tokenSecretID == "" {
			err = c.untilSucceeds(fmt.Sprintf("creating server token for %s - PUT /v1/acl/token", host),
				func() error {
					tokenReq := api.ACLToken{
						Description: tokenDescription,
						Policies:    []*api.ACLTokenPolicyLink{{Name: agentPolicy.Name}},
					}
					token, _, err := serverClient.ACL().TokenCreate(&tokenReq, nil)
					if err != nil {
						return err
					}
					tokenSecretID = token.SecretID
					return nil
				})
			if err != nil {
				return err
			}
		}

		// Pass out agent tokens to servers. It's okay to make this API call
		// even if the server already has a token since the call is idempotent.
		err = c.untilSucceeds(fmt.Sprintf("updating server token for %s - PUT /v1/agent/token/agent", host),
			func() error {
				_, err := serverClient.Agent().UpdateAgentACLToken(tokenSecretID, nil)
				return err
			})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Command) setServerPolicy(consulClient *api.Client) (api.ACLPolicy, error) {
	agentRules, err := c.agentRules()
	if err != nil {
		c.log.Error("Error templating server agent rules", "err", err)
		return api.ACLPolicy{}, err
	}

	// Create agent policy.
	agentPolicy := api.ACLPolicy{
		Name:        "agent-token",
		Description: "Agent Token Policy",
		Rules:       agentRules,
	}
	err = c.untilSucceeds("creating agent policy - PUT /v1/acl/policy",
		func() error {
			return c.createOrUpdateACLPolicy(agentPolicy, consulClient)
		})
	if err != nil {
		return api.ACLPolicy{}, err
	}

	return agentPolicy, nil
}

// isNoLeaderErr returns true if err is due to trying to call the
// bootstrap ACLs API when there is no leader elected.
func isNoLeaderErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), "The ACL system is currently in legacy mode.")
}
