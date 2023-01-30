package kubernetes

import (
	"strconv"

	discovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

type Version struct {
	Major int
	Minor int
}

func GetVersion(cfg *rest.Config) (*Version, error) {
	client, err := discovery.NewDiscoveryClientForConfig(cfg)
	err != nil {
		return nil, err
	}
	version, err := client.ServerVersion()
	if err != nil {
		return nil, err
	}

	major, err := strconv.Atoi(version.Major)
	if err != nil {
		return nil, err
	}

	minor, err := strconv.Atoi(version.Minor)
	if err != nil {
		return nil, err
	}

	return &Version{Major: major, Minor: minor}, nil
}
