package testing

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servergroups"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
)

//nolint:gocyclo // complicated test function
//nolint:funlen // long test function
func GetFakeClient() *MockClient {
	client := MockClient{}

	client.StoredValues = map[string]interface{}{
		"id":          0, // used to generate unique ids across resources
		"groups":      make(map[string]*groups.SecGroup),
		"rules":       make(map[string]*rules.SecGroupRule),
		"fips":        make(map[string]*floatingips.FloatingIP),
		"ports":       make(map[string]*ports.Port),
		"servers":     make(map[string]*servers.Server),
		"servergroup": make(map[string]*servergroups.ServerGroup),
	}

	client.GroupClientObj = &CallbackGroupClient{
		ListFunc: func(_ context.Context, opts groups.ListOpts) ([]groups.SecGroup, error) {
			grps := client.StoredValues["groups"].(map[string]*groups.SecGroup)

			items := make([]groups.SecGroup, 0)
			for _, v := range grps {
				if opts.Name != "" && opts.Name != v.Name {
					// filter by name
					continue
				}

				items = append(items, *v)
			}

			return items, nil
		},
		CreateFunc: func(_ context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error) {
			name := opts.(groups.CreateOpts).Name
			group := &groups.SecGroup{ID: getID(&client), Name: name}

			grps := client.StoredValues["groups"]
			grps.(map[string]*groups.SecGroup)[group.ID] = group

			return group, nil
		},
		GetFunc: func(_ context.Context, id string) (*groups.SecGroup, error) {
			grps := client.StoredValues["groups"]

			group, found := grps.(map[string]*groups.SecGroup)[id]
			if !found {
				return nil, gophercloud.ErrUnexpectedResponseCode{}
			}

			return group, nil
		},
		DeleteFunc: func(_ context.Context, id string) error {
			grps := client.StoredValues["groups"]
			delete(grps.(map[string]*groups.SecGroup), id)
			return nil
		},
		UpdateFunc: func(_ context.Context, _ string, _ groups.UpdateOptsBuilder) (*groups.SecGroup, error) {
			return nil, fmt.Errorf("update group is not implemented yet, we havent used it yet")
		},
	}

	client.RuleClientObj = &CallbackRuleClient{
		ListFunc: func(_ context.Context, opts rules.ListOpts) ([]rules.SecGroupRule, error) {
			rls := client.StoredValues["rules"].(map[string]*rules.SecGroupRule)

			items := make([]rules.SecGroupRule, 0)
			for _, v := range rls {
				if opts.SecGroupID != "" && opts.SecGroupID != v.SecGroupID {
					// filters by SecGroupID
					continue
				}

				if opts.Description != "" && opts.Description != v.Description {
					// filters by SecGroupDescription
					continue
				}

				items = append(items, *v)
			}

			return items, nil
		},
		CreateFunc: func(ctx context.Context, opts rules.CreateOptsBuilder) (*rules.SecGroupRule, error) {
			options := opts.(rules.CreateOpts)

			rule := &rules.SecGroupRule{
				ID:             getID(&client),
				Description:    options.Description,
				SecGroupID:     options.SecGroupID,
				Direction:      string(options.Direction),
				EtherType:      string(options.EtherType),
				Protocol:       string(options.Protocol),
				PortRangeMin:   options.PortRangeMin,
				PortRangeMax:   options.PortRangeMax,
				RemoteIPPrefix: options.RemoteIPPrefix,
				RemoteGroupID:  options.RemoteGroupID,
			}

			rls := client.StoredValues["rules"]
			rls.(map[string]*rules.SecGroupRule)[rule.ID] = rule

			// refresh rules list in group
			for k, v := range client.StoredValues["groups"].(map[string]*groups.SecGroup) {
				if k != rule.SecGroupID {
					continue
				}

				rlsList, _ := client.RuleClientObj.List(
					ctx, rules.ListOpts{SecGroupID: rule.SecGroupID},
				)

				v.Rules = rlsList
			}

			return rule, nil
		},
		GetFunc: func(_ context.Context, id string) (*rules.SecGroupRule, error) {
			rls := client.StoredValues["rules"]
			rule, found := rls.(map[string]*rules.SecGroupRule)[id]
			if !found {
				return nil, gophercloud.ErrUnexpectedResponseCode{}
			}

			return rule, nil
		},
		DeleteFunc: func(_ context.Context, id string) error {
			rls := client.StoredValues["rules"]
			delete(rls.(map[string]*rules.SecGroupRule), id)
			return nil
		},
	}

	client.FipClientObj = &CallbackFipClient{
		ListFunc: func(_ context.Context, optsBuilder floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error) {
			opts := optsBuilder.(floatingips.ListOpts)
			fips := client.StoredValues["fips"].(map[string]*floatingips.FloatingIP)

			items := make([]floatingips.FloatingIP, 0)
			for _, v := range fips {
				if opts.Description != "" && opts.Description != v.Description {
					// filter by description which we use to get fips by fipname
					continue
				}

				if opts.FloatingIP != "" && opts.FloatingIP != v.FloatingIP {
					// filter by floatingip
					continue
				}

				items = append(items, *v)
			}

			return items, nil
		},
		CreateFunc: func(_ context.Context, optsBuilder floatingips.CreateOptsBuilder) (*floatingips.FloatingIP, error) {
			opts := optsBuilder.(floatingips.CreateOpts)
			floatingIP := opts.FloatingIP

			if floatingIP == "" {
				floatingIP = generateIP()
			}

			fip := &floatingips.FloatingIP{
				ID:                getID(&client),
				Description:       opts.Description,
				FloatingNetworkID: opts.FloatingNetworkID,
				FloatingIP:        floatingIP,
			}

			fips := client.StoredValues["fips"]
			fips.(map[string]*floatingips.FloatingIP)[fip.ID] = fip

			return fip, nil
		},
		GetFunc: func(_ context.Context, id string) (*floatingips.FloatingIP, error) {
			fips := client.StoredValues["fips"]
			fip, found := fips.(map[string]*floatingips.FloatingIP)[id]
			if !found {
				return nil, gophercloud.ErrUnexpectedResponseCode{}
			}

			return fip, nil
		},
		DeleteFunc: func(_ context.Context, id string) error {
			fips := client.StoredValues["fips"]
			delete(fips.(map[string]*floatingips.FloatingIP), id)
			return nil
		},
		UpdateFunc: func(ctx context.Context, id string, optsBuilder floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error) {
			opts := optsBuilder.(floatingips.UpdateOpts)

			fip, _ := client.FipClientObj.Get(ctx, id)
			if fip.PortID != *opts.PortID {
				// for now, we only ever update the portID
				fip.PortID = *opts.PortID
			}

			client.StoredValues["fips"].(map[string]*floatingips.FloatingIP)[id] = fip
			return fip, nil
		},
	}

	client.PortClientObj = &CallbackPortClient{
		ListFunc: func(_ context.Context, optsBuilder ports.ListOptsBuilder) ([]ports.Port, error) {
			opts := optsBuilder.(ports.ListOpts)
			prts := client.StoredValues["ports"].(map[string]*ports.Port)

			items := make([]ports.Port, 0)
			for _, v := range prts {
				if opts.Name != "" && opts.Name != v.Name {
					// filter by name is the only thing we use right now
					continue
				}

				items = append(items, *v)
			}

			return items, nil
		},
		CreateFunc: func(_ context.Context, optsBuilder ports.CreateOptsBuilder) (*ports.Port, error) {
			opts := optsBuilder.(ports.CreateOpts)
			var fixedIPs []ports.IP
			if opts.FixedIPs != nil {
				fixedIPs = opts.FixedIPs.([]ports.IP)
			}

			subnetID := "default-subnet-id"
			if len(fixedIPs) > 0 {
				subnetID = fixedIPs[0].SubnetID
			}
			port := &ports.Port{
				ID:        getID(&client),
				Name:      opts.Name,
				NetworkID: opts.NetworkID,
				FixedIPs:  []ports.IP{{SubnetID: subnetID, IPAddress: generateIP()}},
			}

			if opts.SecurityGroups != nil {
				port.SecurityGroups = *opts.SecurityGroups
			}

			prts := client.StoredValues["ports"]
			prts.(map[string]*ports.Port)[port.ID] = port

			return port, nil
		},
		GetFunc: func(_ context.Context, id string) (*ports.Port, error) {
			prts := client.StoredValues["ports"]
			port, found := prts.(map[string]*ports.Port)[id]
			if !found {
				return nil, gophercloud.ErrUnexpectedResponseCode{}
			}

			return port, nil
		},
		DeleteFunc: func(_ context.Context, id string) error {
			prts := client.StoredValues["ports"]
			_, found := prts.(map[string]*ports.Port)[id]
			if !found {
				return gophercloud.ErrUnexpectedResponseCode{}
			}
			delete(prts.(map[string]*ports.Port), id)
			return nil
		},
		UpdateFunc: func(ctx context.Context, id string, optsBuilder ports.UpdateOptsBuilder) (*ports.Port, error) {
			opts := optsBuilder.(ports.UpdateOpts)

			port, _ := client.PortClientObj.Get(ctx, id)

			if opts.SecurityGroups != nil {
				port.SecurityGroups = *opts.SecurityGroups
			}

			if opts.AllowedAddressPairs != nil {
				port.AllowedAddressPairs = *opts.AllowedAddressPairs
			}

			client.StoredValues["ports"].(map[string]*ports.Port)[id] = port
			return port, nil
		},
	}

	client.ServerClientObj = &CallbackServerClient{
		ListFunc: func(_ context.Context, optsBuilder servers.ListOptsBuilder) ([]servers.Server, error) {
			opts := optsBuilder.(servers.ListOpts)
			srvs := client.StoredValues["servers"].(map[string]*servers.Server)

			items := make([]servers.Server, 0)
			for _, v := range srvs {
				if opts.Name != "" && opts.Name != v.Name {
					// filter by name
					continue
				}

				items = append(items, *v)
			}

			return items, nil
		},
		CreateFunc: func(_ context.Context, optsBuilder servers.CreateOptsBuilder, _ servers.SchedulerHintOptsBuilder,
		) (*servers.Server, error) {
			opts := optsBuilder.(*servers.CreateOpts)

			addresses := make(map[string]interface{})
			if nets, ok := opts.Networks.([]servers.Network); ok {
				for i := range nets {
					addresses[nets[i].UUID] = servers.Address{Address: "10.0.0.1"}
				}
			}

			server := &servers.Server{
				ID:        getID(&client),
				Name:      opts.Name,
				Status:    "ACTIVE",
				Created:   time.Now(),
				Addresses: addresses,
			}

			srvs := client.StoredValues["servers"]
			srvs.(map[string]*servers.Server)[server.ID] = server

			return server, nil
		},
		GetFunc: func(_ context.Context, id string) (*servers.Server, error) {
			srvs := client.StoredValues["servers"]
			server, found := srvs.(map[string]*servers.Server)[id]
			if !found {
				return nil, gophercloud.ErrUnexpectedResponseCode{}
			}

			return server, nil
		},
		DeleteFunc: func(_ context.Context, id string) error {
			srvs := client.StoredValues["servers"]
			delete(srvs.(map[string]*servers.Server), id)
			return nil
		},
		UpdateFunc: func(_ context.Context, _ string, _ servers.UpdateOptsBuilder) (*servers.Server, error) {
			// TODO we do not use it yet
			return nil, nil
		},
	}

	client.ServerGroupClientObj = &CallbackServerGroupClient{
		ListFunc: func(_ context.Context, optsBuilder servergroups.ListOptsBuilder) ([]servergroups.ServerGroup, error) {
			_ = optsBuilder.(servergroups.ListOpts)

			srvs := client.StoredValues["servergroup"].(map[string]*servergroups.ServerGroup)

			items := make([]servergroups.ServerGroup, 0)
			for _, v := range srvs {
				items = append(items, *v)
			}

			return items, nil
		},
		CreateFunc: func(_ context.Context, optsBuilder servergroups.CreateOptsBuilder) (*servergroups.ServerGroup, error) {
			opts := optsBuilder.(servergroups.CreateOpts)

			servergroup := &servergroups.ServerGroup{
				ID:       getID(&client),
				Name:     opts.Name,
				Policies: opts.Policies,
			}

			srvs := client.StoredValues["servergroup"]
			srvs.(map[string]*servergroups.ServerGroup)[servergroup.ID] = servergroup

			return servergroup, nil
		},
		GetFunc: func(_ context.Context, id string) (*servergroups.ServerGroup, error) {
			srvs := client.StoredValues["servergroup"]
			server, found := srvs.(map[string]*servergroups.ServerGroup)[id]
			if !found {
				return nil, gophercloud.ErrUnexpectedResponseCode{}
			}

			return server, nil
		},
		DeleteFunc: func(_ context.Context, id string) error {
			srvs := client.StoredValues["servergroup"]
			delete(srvs.(map[string]*servergroups.ServerGroup), id)
			return nil
		},
	}

	return &client
}

func getID(client *MockClient) string {
	id := client.StoredValues["id"].(int)
	client.StoredValues["id"] = id + 1
	return strconv.Itoa(id)
}

func generateIP() string {
	minimum, maximum := 0, 255
	gen := func() string {
		return strconv.Itoa(rand.Intn(maximum-minimum) + minimum) //nolint:gosec // do not need to be secure
	}

	return strings.Join([]string{gen(), gen(), gen(), gen()}, ".")
}
