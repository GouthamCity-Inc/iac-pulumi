package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/c-robinson/iplib"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		var ports []int

		config := config.New(ctx, "")
		vpc_cidr := config.Require("vpc-cidr")
		igw_route := config.Require("igw-route")
		ipv4_cidr := config.Require("ipv4-cidr")
		ipv6_cidr := config.Require("ipv6-cidr")
		ssh_key := config.Require("ssh-key")
		ami_id := config.Require("ami-id")
		config.RequireObject("ports", &ports)

		parts := strings.Split(vpc_cidr, "/")
		ip := parts[0]
		maskStr := parts[1]
		mask, _ := strconv.Atoi(maskStr)

		n := iplib.NewNet4(net.ParseIP(ip), mask)
		subnets, _ := n.Subnet(24)

		subnetStrings := make([]string, len(subnets))
		for i, subnet := range subnets {
			subnetStrings[i] = subnet.String()
		}

		courseTag := pulumi.String("CSYE-6225")
		assignmentTag := pulumi.String("assign-5")

		// tags := pulumi.StringMap{"course": pulumi.String("CSYE-6225"), "assign": pulumi.String("assign-5")}

		vpc, err := ec2.NewVpc(ctx, "vpc", &ec2.VpcArgs{
			CidrBlock:          pulumi.String(vpc_cidr),
			EnableDnsSupport:   pulumi.Bool(true),
			EnableDnsHostnames: pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("vpc-assign-5"),
			},
		})
		ctx.Export("vpcId", vpc.ID())

		if err != nil {
			return err
		}

		var publicSubnets []*ec2.Subnet
		var privateSubnets []*ec2.Subnet

		available, err := aws.GetAvailabilityZones(ctx, &aws.GetAvailabilityZonesArgs{
			State: pulumi.StringRef("available"),
		}, nil)

		if err != nil {
			return err
		}

		for i, az := range available.Names {
			if i < 3 {
				publicSubnetName := "public-subnet-" + fmt.Sprintf("%d", i+1)
				publicSubnet, err := ec2.NewSubnet(ctx, publicSubnetName, &ec2.SubnetArgs{
					VpcId:               vpc.ID(),
					CidrBlock:           pulumi.String(subnetStrings[i]),
					MapPublicIpOnLaunch: pulumi.Bool(true),
					AvailabilityZone:    pulumi.String(az),
					Tags: pulumi.StringMap{
						"course": courseTag,
						"assign": assignmentTag,
						"Name":   pulumi.String(publicSubnetName),
					},
				})
				publicSubnets = append(publicSubnets, publicSubnet)
				if err != nil {
					return err
				}
				privateSubnetName := "private-subnet-" + fmt.Sprintf("%d", i+1)
				privateSubnet, error := ec2.NewSubnet(ctx, privateSubnetName, &ec2.SubnetArgs{
					VpcId:               vpc.ID(),
					CidrBlock:           pulumi.String(subnetStrings[i+3]),
					MapPublicIpOnLaunch: pulumi.Bool(false),
					AvailabilityZone:    pulumi.String(az),
					Tags: pulumi.StringMap{
						"course": courseTag,
						"assign": assignmentTag,
						"Name":   pulumi.String(privateSubnetName),
					},
				})
				privateSubnets = append(privateSubnets, privateSubnet)
				if error != nil {
					return err
				}
			}
		}

		igw, err := ec2.NewInternetGateway(ctx, "internet-gateway", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("internet-gateway"),
			},
		})
		if err != nil {
			return err
		}

		publicRouteTable, err := ec2.NewRouteTable(ctx, "public-route-table", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("public-route-table"),
			},
		})
		if err != nil {
			return err
		}

		privateRouteTable, err := ec2.NewRouteTable(ctx, "private-route-table", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("private-route-table"),
			},
		})
		if err != nil {
			return err
		}

		for i := range available.Names {
			if i < 3 {
				_, err := ec2.NewRouteTableAssociation(ctx, "public-route-table-assoc-"+fmt.Sprintf("%d", i+1), &ec2.RouteTableAssociationArgs{
					SubnetId:     publicSubnets[i].ID(),
					RouteTableId: publicRouteTable.ID(),
				})
				if err != nil {
					return err
				}
			}
		}

		for i := range available.Names {
			if i < 3 {
				_, err := ec2.NewRouteTableAssociation(ctx, "private-route-table-assoc-"+fmt.Sprintf("%d", i+1), &ec2.RouteTableAssociationArgs{
					SubnetId:     privateSubnets[i].ID(),
					RouteTableId: privateRouteTable.ID(),
				})
				if err != nil {
					return err
				}
			}
		}

		_, err = ec2.NewRoute(ctx, "route-to-gateway", &ec2.RouteArgs{
			RouteTableId:         publicRouteTable.ID(),
			DestinationCidrBlock: pulumi.String(igw_route),
			GatewayId:            igw.ID(),
		})
		if err != nil {
			return err
		}

		// create a splice to store instances of type &ec2.SecurityGroupIngressArgs
		var sgIngressRules ec2.SecurityGroupIngressArray

		for i := range ports {
			sgIngressRules = append(sgIngressRules, &ec2.SecurityGroupIngressArgs{
				Protocol: pulumi.String("TCP"),
				ToPort:   pulumi.Int(ports[i]),
				FromPort: pulumi.Int(ports[i]),
				CidrBlocks: pulumi.StringArray{
					pulumi.String(ipv4_cidr),
				},
				Ipv6CidrBlocks: pulumi.StringArray{
					pulumi.String(ipv6_cidr),
				},
			})
		}

		// setup security group
		webappSg, err := ec2.NewSecurityGroup(ctx, "application security group", &ec2.SecurityGroupArgs{
			VpcId:       vpc.ID(),
			Description: pulumi.String("application security group"),
			Ingress:     sgIngressRules,
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("application security group"),
			},
		})
		if err != nil {
			return err
		}

		// spin up an EC2 instance
		_, err = ec2.NewInstance(ctx, "webapp", &ec2.InstanceArgs{
			InstanceType:          pulumi.String("t2.micro"),
			VpcSecurityGroupIds:   pulumi.StringArray{webappSg.ID()},
			SubnetId:              publicSubnets[0].ID(),
			KeyName:               pulumi.String(ssh_key),
			Ami:                   pulumi.String(ami_id),
			DisableApiTermination: pulumi.Bool(false),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("webapp"),
			},
		})
		if err != nil {
			return err
		}

		return nil
	})
}
