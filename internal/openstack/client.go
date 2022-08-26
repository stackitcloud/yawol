package openstack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/ini.v1"
)

// OSClient is an implementation of Client. It must be configured by calling Configure().
// When requesting any specific client the required resources will be created on first call. Mind, that you should
// not call Configure() again, because the created resources will not be invalidated.
type OSClient struct {
	networkV2   *gophercloud.ServiceClient
	computeV2   *gophercloud.ServiceClient
	ini         []byte
	timeout     time.Duration
	promCounter *prometheus.CounterVec
}

// Configures the OSClient with the data of an os auth ini file.
// Used and required flags are auth-url, username, password, domain-name, tenant-name, region in the [global] directive.
//
// Example ini file:
//
//	[Global]
//	auth-url="https://this-is-my-keystone-ep:5000/v3"
//	domain-name="default"
//	tenant-name="mycooltenant"
//	username="itmyuser"
//	password="suupersecret"
//	region="eu01"
func (r *OSClient) Configure(iniBytes []byte, timeout time.Duration, promCounter *prometheus.CounterVec) error {
	r.ini = iniBytes
	r.timeout = timeout
	r.promCounter = promCounter
	return nil
}

// Returns a configured OSFloatingIPClient as FipClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) FipClient(ctx context.Context) (FipClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(ctx, r.ini, r.timeout)
		if err != nil {
			return nil, err
		}
		r.networkV2 = sc
	}

	client := &OSFloatingIPClient{}
	return client.Configure(r.networkV2, r.timeout, r.promCounter), nil
}

// Returns a configured OSPortClient as PortClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) PortClient(ctx context.Context) (PortClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(ctx, r.ini, r.timeout)
		if err != nil {
			return nil, err
		}
		r.networkV2 = sc
	}

	client := &OSPortClient{}
	return client.Configure(r.networkV2, r.timeout, r.promCounter), nil
}

// Returns a configured OSGroupClient as GroupClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) GroupClient(ctx context.Context) (GroupClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(ctx, r.ini, r.timeout)
		if err != nil {
			return nil, err
		}
		r.networkV2 = sc
	}

	client := &OSGroupClient{}
	return client.Configure(r.networkV2, r.timeout, r.promCounter), nil
}

// Returns a configured OSRuleClient as RuleClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) RuleClient(ctx context.Context) (RuleClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(ctx, r.ini, r.timeout)
		if err != nil {
			return nil, err
		}
		r.networkV2 = sc
	}

	client := &OSRuleClient{}
	return client.Configure(r.networkV2, r.timeout, r.promCounter), nil
}

// Returns a configured OSServerClient as ServerClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) ServerClient(ctx context.Context) (ServerClient, error) {
	if r.computeV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createComputeV2FromIni(ctx, r.ini, r.timeout)
		if err != nil {
			return nil, err
		}
		r.computeV2 = sc
	}

	client := &OSServerClient{}
	return client.Configure(r.computeV2, r.timeout, r.promCounter), nil
}

// Returns a configured OSKeypairClient as KeyPairClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) KeyPairClient(ctx context.Context) (KeyPairClient, error) {
	if r.computeV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createComputeV2FromIni(ctx, r.ini, r.timeout)
		if err != nil {
			return nil, err
		}
		r.computeV2 = sc
	}

	client := &OSKeypairClient{}
	return client.Configure(r.computeV2, r.timeout, r.promCounter), nil
}

func createNetworkV2FromIni(ctx context.Context, iniData []byte, timeout time.Duration) (*gophercloud.ServiceClient, error) {
	provider, opts, err := getProvider(ctx, iniData, timeout)
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewNetworkV2(provider, *opts)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func createComputeV2FromIni(ctx context.Context, iniData []byte, timeout time.Duration) (*gophercloud.ServiceClient, error) {
	provider, opts, err := getProvider(ctx, iniData, timeout)
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewComputeV2(provider, *opts)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func getProvider(
	ctx context.Context,
	iniData []byte,
	timeout time.Duration,
) (*gophercloud.ProviderClient, *gophercloud.EndpointOpts, error) {
	cfg, err := ini.Load(iniData)
	if err != nil {
		return nil, nil, err
	}

	var authURL, username, password, domainName, projectName, region string
	authURL = strings.TrimSpace(cfg.Section("Global").Key("auth-url").String())
	username = strings.TrimSpace(cfg.Section("Global").Key("username").String())
	password = strings.TrimSpace(cfg.Section("Global").Key("password").String())
	domainName = strings.TrimSpace(cfg.Section("Global").Key("domain-name").String())
	projectName = strings.TrimSpace(cfg.Section("Global").Key("tenant-name").String())
	region = strings.TrimSpace(cfg.Section("Global").Key("region").String())

	clientOpts := new(clientconfig.ClientOpts)
	authInfo := &clientconfig.AuthInfo{
		AuthURL:     authURL,
		Username:    username,
		Password:    password,
		DomainName:  domainName,
		ProjectName: projectName,
	}
	clientOpts.AuthInfo = authInfo

	ao, err := clientconfig.AuthOptions(clientOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create client auth options: %+v", err)
	}

	provider, err := openstack.NewClient(ao.IdentityEndpoint)
	if err != nil {
		return nil, nil, err
	}

	actx, acancel := context.WithTimeout(ctx, timeout)
	defer acancel()

	provider.Context = actx
	err = openstack.Authenticate(provider, *ao)
	provider.Context = nil
	if err != nil {
		return nil, nil, err
	}

	authProvider := *provider
	authProvider.SetThrowaway(true)
	authProvider.ReauthFunc = nil
	authProvider.SetTokenAndAuthResult(nil)

	authOpts := *ao
	authOpts.AllowReauth = false

	provider.ReauthFunc = func() error {
		pctx, pcancel := context.WithTimeout(ctx, timeout)
		defer pcancel()

		// does not have to be reset since this client is only used in this function
		authProvider.Context = pctx

		eo := gophercloud.EndpointOpts{}
		if strings.Contains(authURL, "v2") {
			if err := openstack.AuthenticateV2(&authProvider, authOpts, eo); err != nil {
				return err
			}
		} else {
			if err := openstack.AuthenticateV3(&authProvider, &authOpts, eo); err != nil {
				return err
			}
		}

		provider.CopyTokenFrom(&authProvider)
		return nil
	}

	return provider, &gophercloud.EndpointOpts{Region: region}, nil
}
