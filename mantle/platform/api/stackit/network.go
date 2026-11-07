// Copyright 2026 Red Hat
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package stackit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

type Network struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type SecurityGroup struct {
	ID string `json:"id"`
}

// CreateNetwork creates a routed DHCP network owned by one kola cluster.
func (a *API) CreateNetwork(name string) (_ *Network, retErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
	defer cancel()
	network := &Network{}
	defer func() {
		if retErr != nil && network.ID != "" {
			if err := a.DeleteNetwork(network.ID); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("cleaning up STACKIT network %s: %w", network.ID, err))
			}
		}
	}()
	labels := a.resourceLabels()
	if err := a.request(ctx, http.MethodPost, a.projectPath("networks"), map[string]interface{}{
		"name": name, "dhcp": true, "routed": true, "labels": labels,
	}, network); err != nil {
		return nil, fmt.Errorf("creating STACKIT network: %w", err)
	}
	if network.ID == "" {
		return nil, errors.New("STACKIT created a network without returning its ID")
	}
	path := a.projectPath("networks/" + url.PathEscape(network.ID))
	if err := a.poll(ctx, func() (bool, error) {
		var current Network
		if err := a.request(ctx, http.MethodGet, path, nil, &current); err != nil {
			if statusIs(err, http.StatusNotFound) {
				return false, nil
			}
			return false, err
		}
		if current.Status == "FAILED" || current.Status == "DELETED" {
			return false, fmt.Errorf("STACKIT network %s entered %s", network.ID, current.Status)
		}
		network.Status = current.Status
		return current.Status == "CREATED", nil
	}); err != nil {
		return nil, fmt.Errorf("waiting for STACKIT network %s: %w", network.ID, err)
	}
	if err := a.ensureResourceLabels(ctx, path, labels); err != nil {
		return nil, fmt.Errorf("labeling STACKIT network %s: %w", network.ID, err)
	}
	return network, nil
}

func (a *API) DeleteNetwork(id string) error {
	if id == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
	defer cancel()
	path := a.projectPath("networks/" + url.PathEscape(id))
	if err := a.delete(ctx, path); err != nil {
		return err
	}
	return a.poll(ctx, func() (bool, error) {
		var current Network
		err := a.request(ctx, http.MethodGet, path, nil, &current)
		if statusIs(err, http.StatusNotFound) {
			return true, nil
		}
		return current.Status == "DELETED", err
	})
}

type securityGroupRule struct {
	Direction             string `json:"direction"`
	Ethertype             string `json:"ethertype,omitempty"`
	Protocol              string `json:"protocol,omitempty"`
	IPRange               string `json:"ipRange,omitempty"`
	RemoteSecurityGroupID string `json:"remoteSecurityGroupId,omitempty"`
	PortRange             *struct {
		Min int `json:"min"`
		Max int `json:"max"`
	} `json:"portRange,omitempty"`
}

// The API accepts a protocol name when creating a rule, but returns an object
// with its name and number when reading one.
type securityGroupRuleState struct {
	securityGroupRule
	Protocol *struct {
		Name   *string `json:"name"`
		Number *int    `json:"number"`
	} `json:"protocol"`
}

// CreateSecurityGroup allows SSH from the runner, cluster traffic within the
// group, and outbound IPv4 traffic for metadata, DNS, and Internet access.
func (a *API) CreateSecurityGroup(name string) (_ *SecurityGroup, retErr error) {
	source := a.opts.SSHSourceCIDR
	if source == "" {
		source = "0.0.0.0/0"
	}
	ip, _, err := net.ParseCIDR(source)
	if err != nil || ip.To4() == nil {
		return nil, errors.New("--stackit-ssh-source-cidr must be an IPv4 CIDR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
	defer cancel()
	group := &SecurityGroup{}
	defer func() {
		if retErr != nil && group.ID != "" {
			if err := a.DeleteSecurityGroup(group.ID); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("cleaning up STACKIT security group %s: %w", group.ID, err))
			}
		}
	}()
	labels := a.resourceLabels()
	if err := a.request(ctx, http.MethodPost, a.projectPath("security-groups"), map[string]interface{}{
		"name": name, "stateful": true, "labels": labels,
	}, group); err != nil {
		return nil, fmt.Errorf("creating STACKIT security group: %w", err)
	}
	if group.ID == "" {
		return nil, errors.New("STACKIT created a security group without returning its ID")
	}
	path := a.projectPath("security-groups/" + url.PathEscape(group.ID))
	if err := a.ensureResourceLabels(ctx, path, labels); err != nil {
		return nil, fmt.Errorf("labeling STACKIT security group %s: %w", group.ID, err)
	}
	// STACKIT may supply default egress rules when creating a group. Inspect
	// those before adding our rule to avoid a duplicate-rule conflict.
	var current struct {
		Rules []securityGroupRuleState `json:"rules"`
	}
	if err := a.request(ctx, http.MethodGet, path, nil, &current); err != nil {
		return nil, fmt.Errorf("reading STACKIT security group %s: %w", group.ID, err)
	}
	ssh := securityGroupRule{Direction: "ingress", Ethertype: "IPv4", Protocol: "tcp", IPRange: source}
	ssh.PortRange = &struct {
		Min int `json:"min"`
		Max int `json:"max"`
	}{22, 22}
	rules := []securityGroupRule{ssh}
	for _, protocol := range []string{"tcp", "udp", "icmp"} {
		rules = append(rules, securityGroupRule{
			Direction: "ingress", Ethertype: "IPv4", Protocol: protocol, RemoteSecurityGroupID: group.ID,
		})
	}
	egressAllowed := false
	for _, rule := range current.Rules {
		allProtocols := rule.Protocol == nil || (rule.Protocol.Name == nil && rule.Protocol.Number == nil)
		if rule.Direction == "egress" && rule.Ethertype == "IPv4" && allProtocols &&
			rule.PortRange == nil && rule.RemoteSecurityGroupID == "" && (rule.IPRange == "" || rule.IPRange == "0.0.0.0/0") {
			egressAllowed = true
		}
	}
	if !egressAllowed {
		rules = append(rules, securityGroupRule{Direction: "egress", Ethertype: "IPv4", IPRange: "0.0.0.0/0"})
	}
	for _, rule := range rules {
		if err := a.request(ctx, http.MethodPost, path+"/rules", rule, nil); err != nil {
			return nil, fmt.Errorf("creating STACKIT security group %s %s %s rule: %w", group.ID, rule.Direction, rule.Protocol, err)
		}
	}
	return group, nil
}

func (a *API) DeleteSecurityGroup(id string) error {
	if id == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
	defer cancel()
	return a.delete(ctx, a.projectPath("security-groups/"+url.PathEscape(id)))
}
