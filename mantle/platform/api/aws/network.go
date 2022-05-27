// Copyright 2018 CoreOS, Inc.
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

package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// getSecurityGroupID gets a security group matching the given name.
// If the security group does not exist, it's created.
func (a *API) getSecurityGroupID(name string) (string, error) {
	// using a Filter on group-name rather than the explicit GroupNames parameter
	// disentangles this call from checking only inside of the default VPC
	sgIds, err := a.ec2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("group-name"),
				Values: []*string{&name},
			},
		},
	})

	if len(sgIds.SecurityGroups) == 0 {
		return a.createSecurityGroup(name)
	}

	if err != nil {
		return "", fmt.Errorf("unable to get security group named %v: %v", name, err)
	}

	return *sgIds.SecurityGroups[0].GroupId, nil
}

// createSecurityGroup creates a security group with tcp/22 access allowed from the
// internet.
func (a *API) createSecurityGroup(name string) (string, error) {
	vpcId, err := a.createVPC(name)
	if err != nil {
		return "", err
	}
	sg, err := a.ec2.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
		GroupName:         aws.String(name),
		Description:       aws.String("mantle security group for testing"),
		VpcId:             aws.String(vpcId),
		TagSpecifications: tagSpecCreatedByMantle(name, ec2.ResourceTypeSecurityGroup),
	})
	if err != nil {
		return "", err
	}
	plog.Debugf("created security group %v", *sg.GroupId)

	allowedIngresses := []ec2.AuthorizeSecurityGroupIngressInput{
		{
			// SSH access from the public internet
			// Full access from inside the same security group
			GroupId: sg.GroupId,
			IpPermissions: []*ec2.IpPermission{
				{
					IpProtocol: aws.String("tcp"),
					IpRanges: []*ec2.IpRange{
						{
							CidrIp: aws.String("0.0.0.0/0"),
						},
					},
					FromPort: aws.Int64(22),
					ToPort:   aws.Int64(22),
				},
				{
					IpProtocol: aws.String("tcp"),
					FromPort:   aws.Int64(1),
					ToPort:     aws.Int64(65535),
					UserIdGroupPairs: []*ec2.UserIdGroupPair{
						{
							GroupId: sg.GroupId,
							VpcId:   &vpcId,
						},
					},
				},
				{
					IpProtocol: aws.String("udp"),
					FromPort:   aws.Int64(1),
					ToPort:     aws.Int64(65535),
					UserIdGroupPairs: []*ec2.UserIdGroupPair{
						{
							GroupId: sg.GroupId,
							VpcId:   &vpcId,
						},
					},
				},
				{
					IpProtocol: aws.String("icmp"),
					FromPort:   aws.Int64(-1),
					ToPort:     aws.Int64(-1),
					UserIdGroupPairs: []*ec2.UserIdGroupPair{
						{
							GroupId: sg.GroupId,
							VpcId:   &vpcId,
						},
					},
				},
			},
		},
	}

	for _, input := range allowedIngresses {
		_, err := a.ec2.AuthorizeSecurityGroupIngress(&input)

		if err != nil {
			// We created the SG but can't add all the needed rules, let's try to
			// bail gracefully
			_, delErr := a.ec2.DeleteSecurityGroup(&ec2.DeleteSecurityGroupInput{
				GroupId: sg.GroupId,
			})
			if delErr != nil {
				return "", fmt.Errorf("created sg %v (%v) but couldn't authorize it. Manual deletion may be required: %v", *sg.GroupId, name, err)
			}
			return "", fmt.Errorf("created sg %v (%v), but couldn't authorize it and thus deleted it: %v", *sg.GroupId, name, err)
		}
	}
	return *sg.GroupId, err
}

// createVPC creates a VPC with an IPV4 CidrBlock of 172.31.0.0/16
func (a *API) createVPC(name string) (string, error) {
	vpc, err := a.ec2.CreateVpc(&ec2.CreateVpcInput{
		AmazonProvidedIpv6CidrBlock: aws.Bool(true),
		CidrBlock:                   aws.String("172.31.0.0/16"),
		TagSpecifications:           tagSpecCreatedByMantle(name, ec2.ResourceTypeVpc),
	})
	if err != nil {
		return "", fmt.Errorf("creating VPC: %v", err)
	}
	if vpc.Vpc == nil || vpc.Vpc.VpcId == nil {
		return "", fmt.Errorf("vpc was nil after creation")
	}

	_, err = a.ec2.ModifyVpcAttribute(&ec2.ModifyVpcAttributeInput{
		EnableDnsHostnames: &ec2.AttributeBooleanValue{
			Value: aws.Bool(true),
		},
		VpcId: vpc.Vpc.VpcId,
	})
	if err != nil {
		return "", fmt.Errorf("enabling DNS Hostnames VPC attribute: %v", err)
	}
	_, err = a.ec2.ModifyVpcAttribute(&ec2.ModifyVpcAttributeInput{
		EnableDnsSupport: &ec2.AttributeBooleanValue{
			Value: aws.Bool(true),
		},
		VpcId: vpc.Vpc.VpcId,
	})
	if err != nil {
		return "", fmt.Errorf("enabling DNS Support VPC attribute: %v", err)
	}

	routeTable, err := a.createRouteTable(name, *vpc.Vpc.VpcId)
	if err != nil {
		return "", fmt.Errorf("creating RouteTable: %v", err)
	}

	err = a.createSubnets(name, *vpc.Vpc.VpcId, routeTable)
	if err != nil {
		return "", fmt.Errorf("creating subnets: %v", err)
	}

	return *vpc.Vpc.VpcId, nil
}

// createRouteTable creates a RouteTable with local targets for subnets for
// destination CIDRs in the VPC as well as an InternetGateway all IPv4/IPv6
// destinations.
func (a *API) createRouteTable(name, vpcId string) (string, error) {
	rt, err := a.ec2.CreateRouteTable(&ec2.CreateRouteTableInput{
		VpcId:             &vpcId,
		TagSpecifications: tagSpecCreatedByMantle(name, ec2.ResourceTypeRouteTable),
	})
	if err != nil {
		return "", err
	}
	if rt.RouteTable == nil || rt.RouteTable.RouteTableId == nil {
		return "", fmt.Errorf("route table was nil after creation")
	}

	igw, err := a.createInternetGateway(name, vpcId)
	if err != nil {
		return "", fmt.Errorf("creating internet gateway: %v", err)
	}

	_, err = a.ec2.CreateRoute(&ec2.CreateRouteInput{
		DestinationCidrBlock: aws.String("0.0.0.0/0"),
		GatewayId:            aws.String(igw),
		RouteTableId:         rt.RouteTable.RouteTableId,
	})
	if err != nil {
		return "", fmt.Errorf("creating remote route: %v", err)
	}

	_, err = a.ec2.CreateRoute(&ec2.CreateRouteInput{
		DestinationIpv6CidrBlock: aws.String("::/0"),
		GatewayId:                aws.String(igw),
		RouteTableId:             rt.RouteTable.RouteTableId,
	})
	if err != nil {
		return "", fmt.Errorf("creating remote route: %v", err)
	}

	return *rt.RouteTable.RouteTableId, nil
}

// creates an InternetGateway and attaches it to the given VPC
func (a *API) createInternetGateway(name, vpcId string) (string, error) {
	igw, err := a.ec2.CreateInternetGateway(&ec2.CreateInternetGatewayInput{
		TagSpecifications: tagSpecCreatedByMantle(name, ec2.ResourceTypeInternetGateway),
	})
	if err != nil {
		return "", err
	}
	if igw.InternetGateway == nil || igw.InternetGateway.InternetGatewayId == nil {
		return "", fmt.Errorf("internet gateway was nil")
	}
	_, err = a.ec2.AttachInternetGateway(&ec2.AttachInternetGatewayInput{
		InternetGatewayId: igw.InternetGateway.InternetGatewayId,
		VpcId:             &vpcId,
	})
	if err != nil {
		return "", fmt.Errorf("attaching internet gateway to vpc: %v", err)
	}
	return *igw.InternetGateway.InternetGatewayId, nil
}

// createSubnets creates a subnet in each availability zone for the region
// that is associated with the given VPC associated with the given RouteTable
func (a *API) createSubnets(name, vpcId, routeTableId string) error {
	azs, err := a.ec2.DescribeAvailabilityZones(&ec2.DescribeAvailabilityZonesInput{})
	if err != nil {
		return fmt.Errorf("retrieving availability zones: %v", err)
	}

	// We need to determine the block of IPv6 addresses that were assigned
	// to us. Let's get that information from the VPC
	request, err := a.ec2.DescribeVpcs(&ec2.DescribeVpcsInput{
		VpcIds: []*string{&vpcId},
	})
	if err != nil {
		return fmt.Errorf("retrieving info about vpc: %v", err)
	}
	vpcIpv6CidrBlock := *request.Vpcs[0].Ipv6CidrBlockAssociationSet[0].Ipv6CidrBlock

	// We were given a /56. When we create subnets they want a /64, which
	// means there are 32 (8 bits) subnets we can create. The loop below only
	// runs 16 times so we only need to pull off the last digit of the hex based
	// ipv6 address (which will be a 0). So we pull off '0::/56' here.
	ipv6CidrBlockPart := vpcIpv6CidrBlock[:len(vpcIpv6CidrBlock)-6]

	for i, az := range azs.AvailabilityZones {
		// 16 is the maximum amount of zones possible when giving them a /20
		// CIDR range inside of a /16 network.
		if i > 15 {
			return nil
		}

		if az.ZoneName == nil {
			continue
		}

		name := *az.ZoneName
		sub, err := a.ec2.CreateSubnet(&ec2.CreateSubnetInput{
			AvailabilityZone:  aws.String(name),
			VpcId:             &vpcId,
			TagSpecifications: tagSpecCreatedByMantle(name, ec2.ResourceTypeSubnet),
			// Increment the CIDR block by 16 every time
			CidrBlock: aws.String(fmt.Sprintf("172.31.%d.0/20", i*16)),
			// Increment the Ipv6CidrBlock by 1 every time (new /64)
			Ipv6CidrBlock: aws.String(fmt.Sprintf("%s%x::/64", ipv6CidrBlockPart, i)),
		})
		if err != nil {
			// Some availability zones get returned but cannot have subnets
			// created inside of them
			if awsErr, ok := (err).(awserr.Error); ok && awsErr.Code() == "InvalidParameterValue" {
				continue
			}
			return fmt.Errorf("creating subnet: %v", err)
		}
		if sub.Subnet == nil || sub.Subnet.SubnetId == nil {
			return fmt.Errorf("subnet was nil after creation")
		}
		_, err = a.ec2.ModifySubnetAttribute(&ec2.ModifySubnetAttributeInput{
			SubnetId: sub.Subnet.SubnetId,
			MapPublicIpOnLaunch: &ec2.AttributeBooleanValue{
				Value: aws.Bool(true),
			},
		})
		if err != nil {
			return err
		}
		_, err = a.ec2.ModifySubnetAttribute(&ec2.ModifySubnetAttributeInput{
			SubnetId: sub.Subnet.SubnetId,
			AssignIpv6AddressOnCreation: &ec2.AttributeBooleanValue{
				Value: aws.Bool(true),
			},
		})
		if err != nil {
			return err
		}

		_, err = a.ec2.AssociateRouteTable(&ec2.AssociateRouteTableInput{
			RouteTableId: &routeTableId,
			SubnetId:     sub.Subnet.SubnetId,
		})
		if err != nil {
			return fmt.Errorf("associating subnet with route table: %v", err)
		}
	}

	return nil
}

// getSubnetID gets a subnet for the given VPC and availability zone
func (a *API) getSubnetID(vpc string, zone string) (string, error) {
	subIds, err := a.ec2.DescribeSubnets(&ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{&vpc},
			},
			{
				Name:   aws.String("availability-zone"),
				Values: []*string{&zone},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("unable to get subnets for vpc/zone %v/%v: %v", vpc, zone, err)
	}
	for _, id := range subIds.Subnets {
		if id.SubnetId != nil {
			return *id.SubnetId, nil
		}
	}
	return "", fmt.Errorf("no subnets found for vpc %v", vpc)
}

// getVPCID gets a VPC for the given security group
func (a *API) getVPCID(sgId string) (string, error) {
	sgs, err := a.ec2.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{&sgId},
	})
	if err != nil {
		return "", fmt.Errorf("listing vpc's: %v", err)
	}
	for _, sg := range sgs.SecurityGroups {
		if sg.VpcId != nil {
			return *sg.VpcId, nil
		}
	}
	return "", fmt.Errorf("no vpc found for security group %v", sgId)
}
