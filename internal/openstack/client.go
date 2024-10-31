package openstack

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/utils/v2/openstack/clientconfig"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/ini.v1"
)

type GetOSClientFunc func(iniData []byte, overwrite OSClientOverwrite) (Client, error)

type OSClientOverwrite struct {
	ProjectID *string
}

// OSClient is an implementation of Client. It must be configured by calling Configure().
// When requesting any specific client the required resources will be created on first call. Mind, that you should
// not call Configure() again, because the created resources will not be invalidated.
type OSClient struct {
	networkV2   *gophercloud.ServiceClient
	computeV2   *gophercloud.ServiceClient
	ini         []byte
	overwrite   OSClientOverwrite
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
func (r *OSClient) Configure(
	iniBytes []byte,
	overwrite OSClientOverwrite,
	timeout time.Duration,
	promCounter *prometheus.CounterVec,
) error {
	r.ini = iniBytes
	r.timeout = timeout
	r.promCounter = promCounter
	r.overwrite = overwrite
	return nil
}

// Returns a configured OSFloatingIPClient as FipClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) FipClient(ctx context.Context) (FipClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(ctx, r.ini, r.overwrite, r.timeout)
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
		sc, err := createNetworkV2FromIni(ctx, r.ini, r.overwrite, r.timeout)
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
		sc, err := createNetworkV2FromIni(ctx, r.ini, r.overwrite, r.timeout)
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
		sc, err := createNetworkV2FromIni(ctx, r.ini, r.overwrite, r.timeout)
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
		sc, err := createComputeV2FromIni(ctx, r.ini, r.overwrite, r.timeout)
		if err != nil {
			return nil, err
		}
		r.computeV2 = sc
	}

	client := &OSServerClient{}
	return client.Configure(r.computeV2, r.timeout, r.promCounter), nil
}

// Returns a configured OSServerGroupClient as ServerGroupClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) ServerGroupClient(ctx context.Context) (ServerGroupClient, error) {
	if r.computeV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createComputeV2FromIni(ctx, r.ini, r.overwrite, r.timeout)
		if err != nil {
			return nil, err
		}
		r.computeV2 = sc
	}

	client := &OSServerGroupClient{}
	return client.Configure(r.computeV2, r.timeout, r.promCounter), nil
}

// Returns a configured OSKeypairClient as KeyPairClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) KeyPairClient(ctx context.Context) (KeyPairClient, error) {
	if r.computeV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createComputeV2FromIni(ctx, r.ini, r.overwrite, r.timeout)
		if err != nil {
			return nil, err
		}
		r.computeV2 = sc
	}

	client := &OSKeypairClient{}
	return client.Configure(r.computeV2, r.timeout, r.promCounter), nil
}

func createNetworkV2FromIni(
	ctx context.Context,
	iniData []byte,
	overwrite OSClientOverwrite,
	timeout time.Duration,
) (*gophercloud.ServiceClient, error) {
	provider, opts, err := getProvider(ctx, iniData, overwrite, timeout)
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewNetworkV2(provider, *opts)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func createComputeV2FromIni(
	ctx context.Context,
	iniData []byte,
	overwrite OSClientOverwrite,
	timeout time.Duration,
) (*gophercloud.ServiceClient, error) {
	provider, opts, err := getProvider(ctx, iniData, overwrite, timeout)
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
	overwrite OSClientOverwrite,
	timeout time.Duration,
) (*gophercloud.ProviderClient, *gophercloud.EndpointOpts, error) {
	cfg, err := ini.LoadSources(
		ini.LoadOptions{IgnoreInlineComment: true},
		iniData,
	)
	if err != nil {
		return nil, nil, err
	}

	var authInfo clientconfig.AuthInfo

	authURL := strings.TrimSpace(cfg.Section("Global").Key("auth-url").String())
	authInfo.AuthURL = authURL
	authInfo.Username = strings.TrimSpace(cfg.Section("Global").Key("username").String())
	authInfo.Password = strings.TrimSpace(cfg.Section("Global").Key("password").String())

	authInfo.DomainName = strings.TrimSpace(cfg.Section("Global").Key("domain-name").String())
	authInfo.DomainID = strings.TrimSpace(cfg.Section("Global").Key("domain-id").String())

	legacyProjectName := strings.TrimSpace(cfg.Section("Global").Key("tenant-name").String())
	projectName := strings.TrimSpace(cfg.Section("Global").Key("project-name").String())
	authInfo.ProjectID = strings.TrimSpace(cfg.Section("Global").Key("project-id").String())

	caFile := strings.TrimSpace(cfg.Section("Global").Key("ca-file").String())

	// TODO: remove legacyProjectName once openstack-cloud-controller has dropped tenant-name support. Link to ccm args:
	//nolint:lll // link
	// https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/openstack-cloud-controller-manager/using-openstack-cloud-controller-manager.md
	if projectName != "" {
		authInfo.ProjectName = projectName
	} else {
		authInfo.ProjectName = legacyProjectName
	}

	region := strings.TrimSpace(cfg.Section("Global").Key("region").String())

	if overwrite.ProjectID != nil {
		authInfo.ProjectName = ""
		authInfo.ProjectID = *overwrite.ProjectID
	}

	// construct transport that trusts the configured CA bundle
	var transport http.RoundTripper

	// If OS_CACERT env var is set it takes precedence over the configuration.
	// This is useful for running yawol-controller locally where the configured file name in the cloud-provider config
	// might not match with the local environment.
	if caFileEnv := os.Getenv("OS_CACERT"); caFileEnv != "" {
		caFile = caFileEnv
	}

	if caFile != "" {
		roots, err := certutil.NewPool(caFile)
		if err != nil {
			return nil, nil, err
		}
		config := &tls.Config{MinVersion: tls.VersionTLS12}
		config.RootCAs = roots
		transport = netutil.SetOldTransportDefaults(&http.Transport{TLSClientConfig: config})
	}

	clientOpts := new(clientconfig.ClientOpts)
	clientOpts.AuthInfo = &authInfo

	ao, err := clientconfig.AuthOptions(clientOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create client auth options: %+v", err)
	}

	provider, err := openstack.NewClient(ao.IdentityEndpoint)
	if err != nil {
		return nil, nil, err
	}

	if transport != nil {
		provider.HTTPClient.Transport = transport
	}

	actx, acancel := context.WithTimeout(ctx, timeout)
	defer acancel()

	err = openstack.Authenticate(actx, provider, *ao)
	if err != nil {
		return nil, nil, err
	}

	authProvider := *provider
	authProvider.SetThrowaway(true)
	authProvider.ReauthFunc = nil
	authProvider.SetTokenAndAuthResult(nil)

	authOpts := *ao
	authOpts.AllowReauth = false

	provider.ReauthFunc = func(context.Context) error {
		pctx, pcancel := context.WithTimeout(ctx, timeout)
		defer pcancel()

		eo := gophercloud.EndpointOpts{}
		if strings.Contains(authURL, "v2") {
			if err := openstack.AuthenticateV2(pctx, &authProvider, authOpts, eo); err != nil {
				return err
			}
		} else {
			if err := openstack.AuthenticateV3(pctx, &authProvider, &authOpts, eo); err != nil {
				return err
			}
		}

		provider.CopyTokenFrom(&authProvider)
		return nil
	}

	return provider, &gophercloud.EndpointOpts{Region: region}, nil
}
