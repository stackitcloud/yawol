package openstack

import (
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"gopkg.in/ini.v1"
)

// OSClient is an implementation of Client. It must be configured by calling Configure().
// When requesting any specific client the required resources will be created on first call. Mind, that you should
// not call Configure() again, because the created resources will not be invalidated.
type OSClient struct {
	networkV2      *gophercloud.ServiceClient
	computeV2      *gophercloud.ServiceClient
	loadBalancerV2 *gophercloud.ServiceClient
	ini            []byte
}

// Configures the OSClient with the data of an os auth ini file.
// Used and required flags are auth-url, username, password, domain-name, tenant-name, region in the [global] directive.
//
// Example ini file:
//	[Global]
//	auth-url="https://this-is-my-keystone-ep:5000/v3"
//	domain-name="default"
//	tenant-name="mycooltenant"
//	username="itmyuser"
//	password="suupersecret"
//	region="eu01"
func (r *OSClient) Configure(ini []byte) error {
	r.ini = ini
	return nil
}

// Returns a configured OSFloatingIPClient as FipClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) FipClient() (FipClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(r.ini)
		if err != nil {
			return nil, err
		}
		r.networkV2 = sc
	}

	client := &OSFloatingIPClient{}
	return client.Configure(r.networkV2), nil
}

// Returns a configured OSPortClient as PortClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) PortClient() (PortClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(r.ini)
		if err != nil {
			return nil, err
		}
		r.networkV2 = sc
	}

	client := &OSPortClient{}
	return client.Configure(r.networkV2), nil

}

// Returns a configured OSGroupClient as GroupClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) GroupClient() (GroupClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(r.ini)
		if err != nil {
			return nil, err
		}
		r.networkV2 = sc
	}

	client := &OSGroupClient{}
	return client.Configure(r.networkV2), nil
}

// Returns a configured OSRuleClient as RuleClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) RuleClient() (RuleClient, error) {
	if r.networkV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createNetworkV2FromIni(r.ini)
		if err != nil {
			return nil, err
		}
		r.networkV2 = sc
	}

	client := &OSRuleClient{}
	return client.Configure(r.networkV2), nil
}

// Returns a configured OSServerClient as ServerClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) ServerClient() (ServerClient, error) {
	if r.computeV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createComputeV2FromIni(r.ini)
		if err != nil {
			return nil, err
		}
		r.computeV2 = sc
	}

	client := &OSServerClient{}
	return client.Configure(r.computeV2), nil
}

// Returns a configured OSKeypairClient as KeyPairClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) KeyPairClient() (KeyPairClient, error) {
	if r.computeV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createComputeV2FromIni(r.ini)
		if err != nil {
			return nil, err
		}
		r.computeV2 = sc
	}

	client := &OSKeypairClient{}
	return client.Configure(r.computeV2), nil
}

// Returns a configured OSAttachInterfacesClient as AttachInterfaceClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) AttachInterfaceClient() (AttachInterfaceClient, error) {
	if r.computeV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createComputeV2FromIni(r.ini)
		if err != nil {
			return nil, err
		}
		r.computeV2 = sc
	}

	client := &OSAttachInterfacesClient{}
	return client.Configure(r.computeV2), nil
}

// Returns a configured OSAttachInterfacesClient as AttachInterfaceClient.
// Make sure that you invoked Configure() before this.
func (r *OSClient) LoadBalancerClient() (LoadBalancerClient, error) {
	if r.computeV2 == nil {
		var sc *gophercloud.ServiceClient
		sc, err := createLoadbalancerV2FromIni(r.ini)
		if err != nil {
			return nil, err
		}
		r.loadBalancerV2 = sc
	}

	client := &OSLoadBalancerClient{}
	return client.Configure(r.loadBalancerV2), nil
}

func createNetworkV2FromIni(iniData []byte) (*gophercloud.ServiceClient, error) {
	provider, opts, err := getProvider(iniData)
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewNetworkV2(provider, *opts)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func createComputeV2FromIni(iniData []byte) (*gophercloud.ServiceClient, error) {
	provider, opts, err := getProvider(iniData)
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewComputeV2(provider, *opts)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func createLoadbalancerV2FromIni(iniData []byte) (*gophercloud.ServiceClient, error) {
	provider, opts, err := getProvider(iniData)
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewLoadBalancerV2(provider, *opts)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func getProvider(iniData []byte) (*gophercloud.ProviderClient, *gophercloud.EndpointOpts, error) {
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

	provider, err := openstack.AuthenticatedClient(*ao)
	if err != nil {
		return nil, nil, err
	}

	return provider, &gophercloud.EndpointOpts{Region: region}, nil
}
