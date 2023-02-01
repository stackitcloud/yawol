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

func (v *Version) IsGreater(version *Version) bool {
	if v.Major > version.Major {
		return true
	}

	if v.Major < version.Major {
		return false
	}

	if v.Minor > version.Minor {
		return true
	}

	if v.Minor < version.Minor {
		return false
	}

	// versions are equal
	return false
}

func (v *Version) IsEqual(version *Version) bool {
	return (v.Major == version.Major &&
		v.Minor == version.Minor)
}

func (v *Version) IsLower(version *Version) bool {
	if v.IsGreater(version) {
		return false
	}

	if v.IsEqual(version) {
		return false
	}

	return true
}
