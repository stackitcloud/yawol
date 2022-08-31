package keepalived

import (
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

type StatsEntry struct {
	VRRPInstance `yaml:"VRRP Instance"`
}

type VRRPInstance struct {
	Advertisements struct {
		Received int `yaml:"Received"`
		Sent     int `yaml:"Sent"`
	} `yaml:"Advertisements"`
	BecameMaster   int `yaml:"Became master"`
	ReleasedMaster int `yaml:"Released master"`
	PacketErrors   struct {
		Length                int `yaml:"Length"`
		TTL                   int `yaml:"TTL"`
		InvalidType           int `yaml:"Invalid Type"`
		AdvertisementInterval int `yaml:"Advertisement Interval"`
		AddressList           int `yaml:"Address List"`
	} `yaml:"Packet Errors"`
	AuthenticationErrors struct {
		InvalidType  int `yaml:"Invalid Type"`
		TypeMismatch int `yaml:"Type Mismatch"`
		Failure      int `yaml:"Failure"`
	} `yaml:"Authentication Errors"`
	PriorityZero struct {
		Received int `yaml:"Received"`
		Sent     int `yaml:"Sent"`
	} `yaml:"Priority Zero"`
}

type Stats map[string]VRRPInstance

func ParseStats(statsContent string) (Stats, error) {
	instances := make(map[string]string)
	currentInstanceName := ""
	for _, line := range strings.Split(statsContent, "\n") {
		if strings.HasPrefix(line, "VRRP Instance: ") {
			currentInstanceName = strings.TrimPrefix(line, "VRRP Instance: ")
			instances[currentInstanceName] += "VRRP Instance: \n"
			continue
		}
		if currentInstanceName != "" {
			instances[currentInstanceName] += line + "\n"
		}
	}

	stats := make(Stats)
	for instance, vrrpStats := range instances {
		s := StatsEntry{}
		err := yaml.Unmarshal([]byte(vrrpStats), &s)
		if err != nil {
			return nil, err
		}
		stats[instance] = s.VRRPInstance
	}
	return stats, nil
}

// ReadStatsFromDisk returns stats file as String and last modified time
func ReadStatsFromDisk(keepalivedStatsFile string) (string, time.Time, error) {
	statFile, err := os.ReadFile(keepalivedStatsFile)
	if err != nil {
		return "", time.Time{}, err
	}
	fileStat, err := os.Stat(keepalivedStatsFile)
	if err != nil {
		return "", time.Time{}, err
	}
	return string(statFile), fileStat.ModTime(), nil
}

func (v VRRPInstance) IsMaster() bool {
	return v.BecameMaster > v.ReleasedMaster
}

// ReadStatsForInstanceName returns VRRP Stats for an Instance and last modified time from the stats file
func ReadStatsForInstanceName(instanceName, filename string) (VRRPInstance, time.Time, error) {
	statsFile, modTime, err := ReadStatsFromDisk(filename)
	if err != nil {
		return VRRPInstance{}, time.Time{}, err
	}

	stats, err := ParseStats(statsFile)
	if err != nil {
		return VRRPInstance{}, time.Time{}, err
	}

	return stats[instanceName], modTime, nil
}
