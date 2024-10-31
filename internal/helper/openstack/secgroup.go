package openstack

import (
	"context"
	"strings"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/stackitcloud/yawol/internal/openstack"
)

// CreateSecGroup creates a SecGroup and returns it.
func CreateSecGroup(
	ctx context.Context,
	groupClient openstack.GroupClient,
	name string,
) (*groups.SecGroup, error) {
	secGroup, err := groupClient.Create(ctx, groups.CreateOpts{
		Name: name,
	})
	if err != nil {
		return nil, err
	}
	return secGroup, nil
}

// DeleteSecGroup deletes a SecGroup by ID.
func DeleteSecGroup(
	ctx context.Context,
	groupClient openstack.GroupClient,
	secGroupID string,
) error {
	return groupClient.Delete(ctx, secGroupID)
}

// GetSecGroupByName returns a Port filtered By Name.
// Returns an error on connection issues.
// Returns nil if not found.
func GetSecGroupByName(
	ctx context.Context,
	groupClient openstack.GroupClient,
	groupName string,
) (*groups.SecGroup, error) {
	groupList, err := groupClient.List(ctx, groups.ListOpts{Name: groupName})
	if err != nil {
		return nil, err
	}

	for i := range groupList {
		if groupList[i].Name == groupName {
			return &groupList[i], nil
		}
	}
	return nil, nil
}

// GetSecGroupByID returns a secGroup by an openstack ID.
// Returns an error on connection issues.
// Returns err if not found.
func GetSecGroupByID(
	ctx context.Context,
	secGroupClient openstack.GroupClient,
	secGroupID string,
) (*groups.SecGroup, error) {
	secGroup, err := secGroupClient.Get(ctx, secGroupID)
	if err != nil {
		return nil, err
	}
	return secGroup, err
}

func DeleteSecGroupRule(
	ctx context.Context,
	ruleClient openstack.RuleClient,
	ruleID string,
) error {
	return ruleClient.Delete(ctx, ruleID)
}

func CreateSecGroupRule(
	ctx context.Context,
	ruleClient openstack.RuleClient,
	secGroupID string,
	namespacedName string,
	rule *rules.SecGroupRule,
) error {
	_, err := ruleClient.Create(ctx, rules.CreateOpts{
		SecGroupID:     secGroupID,
		Description:    namespacedName,
		Direction:      rules.RuleDirection(rule.Direction),
		EtherType:      rules.RuleEtherType(rule.EtherType),
		Protocol:       rules.RuleProtocol(rule.Protocol),
		PortRangeMax:   rule.PortRangeMax,
		PortRangeMin:   rule.PortRangeMin,
		RemoteIPPrefix: rule.RemoteIPPrefix,
		RemoteGroupID:  rule.RemoteGroupID,
	})
	return err
}

// DeleteUnusedSecGroupRulesFromSecGroup deletes rules that are not used anymore.
// Deletion must happen before creation in order to not have temporary duplicated / overlapping rules
func DeleteUnusedSecGroupRulesFromSecGroup(
	ctx context.Context,
	ruleClient openstack.RuleClient,
	secGroup *groups.SecGroup,
	desiredSecGroupRules []rules.SecGroupRule,
) error {
	for i := range secGroup.Rules {
		found := false
		for y := range desiredSecGroupRules {
			if SecGroupRuleIsEqual(&secGroup.Rules[i], &desiredSecGroupRules[y]) {
				found = true
				break
			}
		}

		if found {
			continue
		}

		err := DeleteSecGroupRule(ctx, ruleClient, secGroup.Rules[i].ID)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateNonExistingSecGroupRules create desiredSecGroupRules if not present in secGroup.
func CreateNonExistingSecGroupRules(
	ctx context.Context,
	ruleClient openstack.RuleClient,
	namespacedName string,
	secGroup *groups.SecGroup,
	desiredSecGroupRules []rules.SecGroupRule,
) error {
	for i := range desiredSecGroupRules {
		var isApplied bool
		for y := range secGroup.Rules {
			if SecGroupRuleIsEqual(&secGroup.Rules[y], &desiredSecGroupRules[i]) {
				isApplied = true
				break
			}
		}

		if isApplied {
			continue
		}

		err := CreateSecGroupRule(ctx, ruleClient, secGroup.ID, namespacedName, &desiredSecGroupRules[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// SecGroupRuleIsEqual returns true if both SecGroupRules are equal.
func SecGroupRuleIsEqual(first, second *rules.SecGroupRule) bool {
	if strings.EqualFold(first.EtherType, second.EtherType) &&
		strings.EqualFold(first.Direction, second.Direction) &&
		strings.EqualFold(first.Protocol, second.Protocol) &&
		first.RemoteIPPrefix == second.RemoteIPPrefix &&
		first.PortRangeMax == second.PortRangeMax &&
		first.PortRangeMin == second.PortRangeMin &&
		first.RemoteGroupID == second.RemoteGroupID {
		return true
	}

	return false
}
