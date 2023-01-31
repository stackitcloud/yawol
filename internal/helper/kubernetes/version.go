package kubernetes

import (
	"strconv"

	discovery "k8s.io/client-go/discovery"
)

type Version struct {
	Major int
	Minor int
}

func GetVersion(client *discovery.DiscoveryClient) (*Version, error) {
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
