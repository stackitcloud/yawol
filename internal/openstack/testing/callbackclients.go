package testing

import (
	"context"

	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servergroups"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
)

type CallbackGroupClient struct {
	ListFunc   func(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error)
	CreateFunc func(ctx context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error)
	GetFunc    func(ctx context.Context, id string) (*groups.SecGroup, error)
	UpdateFunc func(ctx context.Context, id string, opts groups.UpdateOptsBuilder) (*groups.SecGroup, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackGroupClient) List(ctx context.Context, opts groups.ListOpts) ([]groups.SecGroup, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackGroupClient) Create(ctx context.Context, opts groups.CreateOptsBuilder) (*groups.SecGroup, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackGroupClient) Update(ctx context.Context, id string, opts groups.UpdateOptsBuilder) (*groups.SecGroup, error) {
	return r.UpdateFunc(ctx, id, opts)
}
func (r *CallbackGroupClient) Get(ctx context.Context, id string) (*groups.SecGroup, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackGroupClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackRuleClient struct {
	ListFunc   func(ctx context.Context, opts rules.ListOpts) ([]rules.SecGroupRule, error)
	CreateFunc func(ctx context.Context, opts rules.CreateOptsBuilder) (*rules.SecGroupRule, error)
	GetFunc    func(ctx context.Context, id string) (*rules.SecGroupRule, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackRuleClient) List(ctx context.Context, opts rules.ListOpts) ([]rules.SecGroupRule, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackRuleClient) Create(ctx context.Context, opts rules.CreateOptsBuilder) (*rules.SecGroupRule, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackRuleClient) Get(ctx context.Context, id string) (*rules.SecGroupRule, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackRuleClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackFipClient struct {
	ListFunc   func(ctx context.Context, opts floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error)
	CreateFunc func(ctx context.Context, opts floatingips.CreateOptsBuilder) (*floatingips.FloatingIP, error)
	UpdateFunc func(ctx context.Context, id string, opts floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error)
	GetFunc    func(ctx context.Context, id string) (*floatingips.FloatingIP, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackFipClient) List(ctx context.Context, opts floatingips.ListOptsBuilder) ([]floatingips.FloatingIP, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackFipClient) Create(ctx context.Context, opts floatingips.CreateOptsBuilder) (*floatingips.FloatingIP, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackFipClient) Update(ctx context.Context, id string, opts floatingips.UpdateOptsBuilder) (*floatingips.FloatingIP, error) {
	return r.UpdateFunc(ctx, id, opts)
}
func (r *CallbackFipClient) Get(ctx context.Context, id string) (*floatingips.FloatingIP, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackFipClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackPortClient struct {
	ListFunc   func(ctx context.Context, opts ports.ListOptsBuilder) ([]ports.Port, error)
	GetFunc    func(ctx context.Context, id string) (*ports.Port, error)
	CreateFunc func(ctx context.Context, opts ports.CreateOptsBuilder) (*ports.Port, error)
	UpdateFunc func(ctx context.Context, id string, opts ports.UpdateOptsBuilder) (*ports.Port, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackPortClient) List(ctx context.Context, opts ports.ListOptsBuilder) ([]ports.Port, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackPortClient) Get(ctx context.Context, id string) (*ports.Port, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackPortClient) Create(ctx context.Context, opts ports.CreateOptsBuilder) (*ports.Port, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackPortClient) Update(ctx context.Context, id string, opts ports.UpdateOptsBuilder) (*ports.Port, error) {
	return r.UpdateFunc(ctx, id, opts)
}
func (r *CallbackPortClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackServerClient struct {
	ListFunc   func(ctx context.Context, opts servers.ListOptsBuilder) ([]servers.Server, error)
	CreateFunc func(ctx context.Context, opts servers.CreateOptsBuilder, hintOpts servers.SchedulerHintOptsBuilder) (*servers.Server, error)
	GetFunc    func(ctx context.Context, id string) (*servers.Server, error)
	UpdateFunc func(ctx context.Context, id string, opts servers.UpdateOptsBuilder) (*servers.Server, error)
	DeleteFunc func(ctx context.Context, id string) error
}

func (r *CallbackServerClient) List(ctx context.Context, opts servers.ListOptsBuilder) ([]servers.Server, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackServerClient) Create(ctx context.Context, opts servers.CreateOptsBuilder, hintOpts servers.SchedulerHintOptsBuilder,
) (*servers.Server, error) {
	return r.CreateFunc(ctx, opts, hintOpts)
}
func (r *CallbackServerClient) Get(ctx context.Context, id string) (*servers.Server, error) {
	return r.GetFunc(ctx, id)
}
func (r *CallbackServerClient) Update(ctx context.Context, id string, opts servers.UpdateOptsBuilder) (*servers.Server, error) {
	return r.UpdateFunc(ctx, id, opts)
}
func (r *CallbackServerClient) Delete(ctx context.Context, id string) error {
	return r.DeleteFunc(ctx, id)
}

type CallbackKeypairClient struct {
	ListFunc   func(ctx context.Context) ([]keypairs.KeyPair, error)
	CreateFunc func(ctx context.Context, opts keypairs.CreateOptsBuilder) (*keypairs.KeyPair, error)
	GetFunc    func(ctx context.Context, name string) (*keypairs.KeyPair, error)
	DeleteFunc func(ctx context.Context, name string) error
}

func (r *CallbackKeypairClient) List(ctx context.Context) ([]keypairs.KeyPair, error) {
	return r.ListFunc(ctx)
}
func (r *CallbackKeypairClient) Create(ctx context.Context, opts keypairs.CreateOptsBuilder) (*keypairs.KeyPair, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackKeypairClient) Get(ctx context.Context, name string) (*keypairs.KeyPair, error) {
	return r.GetFunc(ctx, name)
}
func (r *CallbackKeypairClient) Delete(ctx context.Context, name string) error {
	return r.DeleteFunc(ctx, name)
}

type CallbackServerGroupClient struct {
	ListFunc   func(ctx context.Context, opts servergroups.ListOptsBuilder) ([]servergroups.ServerGroup, error)
	CreateFunc func(ctx context.Context, opts servergroups.CreateOptsBuilder) (*servergroups.ServerGroup, error)
	GetFunc    func(ctx context.Context, name string) (*servergroups.ServerGroup, error)
	DeleteFunc func(ctx context.Context, name string) error
}

func (r *CallbackServerGroupClient) List(ctx context.Context, opts servergroups.ListOptsBuilder) ([]servergroups.ServerGroup, error) {
	return r.ListFunc(ctx, opts)
}
func (r *CallbackServerGroupClient) Create(ctx context.Context, opts servergroups.CreateOptsBuilder) (*servergroups.ServerGroup, error) {
	return r.CreateFunc(ctx, opts)
}
func (r *CallbackServerGroupClient) Get(ctx context.Context, name string) (*servergroups.ServerGroup, error) {
	return r.GetFunc(ctx, name)
}
func (r *CallbackServerGroupClient) Delete(ctx context.Context, name string) error {
	return r.DeleteFunc(ctx, name)
}
