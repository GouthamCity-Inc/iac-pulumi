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

		config := config.New(ctx, "")
		vpc_cidr := config.Require("vpc-cidr")
		igw_route := config.Require("igw-route")
		dev_account_id := config.Require("dev-account-id")
		ipv4_cidr := config.Require("ipv4-cidr")
		ipv6_cidr := config.Require("ipv6-cidr")
		ssh_key := config.Require("ssh-key")

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

		tags := pulumi.StringMap{"course": pulumi.String("CSYE-6225"), "assign": pulumi.String("assign-5")}

		vpc, err := ec2.NewVpc(ctx, "vpc", &ec2.VpcArgs{
			CidrBlock:          pulumi.String(vpc_cidr),
			EnableDnsSupport:   pulumi.Bool(true),
			EnableDnsHostnames: pulumi.Bool(true),
			Tags:               tags,
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

				publicSubnet, err := ec2.NewSubnet(ctx, "public-subnet-"+fmt.Sprintf("%d", i+1), &ec2.SubnetArgs{
					VpcId:               vpc.ID(),
					CidrBlock:           pulumi.String(subnetStrings[i]),
					MapPublicIpOnLaunch: pulumi.Bool(true),
					AvailabilityZone:    pulumi.String(az),
					Tags:                tags,
				})
				publicSubnets = append(publicSubnets, publicSubnet)
				if err != nil {
					return err
				}

				privateSubnet, error := ec2.NewSubnet(ctx, "private-subnet-"+fmt.Sprintf("%d", i+1), &ec2.SubnetArgs{
					VpcId:               vpc.ID(),
					CidrBlock:           pulumi.String(subnetStrings[i+3]),
					MapPublicIpOnLaunch: pulumi.Bool(false),
					AvailabilityZone:    pulumi.String(az),
					Tags:                tags,
				})
				privateSubnets = append(privateSubnets, privateSubnet)
				if error != nil {
					return err
				}
			}
		}

		igw, err := ec2.NewInternetGateway(ctx, "internet-gateway", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags:  tags,
		})
		if err != nil {
			return err
		}

		publicRouteTable, err := ec2.NewRouteTable(ctx, "public-route-table", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags:  tags,
		})
		if err != nil {
			return err
		}

		privateRouteTable, err := ec2.NewRouteTable(ctx, "private-route-table", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags:  tags,
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

		// setup security group
		webappSg, err := ec2.NewSecurityGroup(ctx, "application security group", &ec2.SecurityGroupArgs{
			Tags:        tags,
			VpcId:       vpc.ID(),
			Description: pulumi.String("application security group"),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					Protocol: pulumi.String("TCP"),
					ToPort:   pulumi.Int(80),
					FromPort: pulumi.Int(80),
					CidrBlocks: pulumi.StringArray{
						pulumi.String(ipv4_cidr),
					},
					Ipv6CidrBlocks: pulumi.StringArray{
						pulumi.String(ipv6_cidr),
					},
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol: pulumi.String("TCP"),
					ToPort:   pulumi.Int(22),
					FromPort: pulumi.Int(22),
					CidrBlocks: pulumi.StringArray{
						pulumi.String(ipv4_cidr),
					},
					Ipv6CidrBlocks: pulumi.StringArray{
						pulumi.String(ipv6_cidr),
					},
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol: pulumi.String("TCP"),
					ToPort:   pulumi.Int(443),
					FromPort: pulumi.Int(443),
					CidrBlocks: pulumi.StringArray{
						pulumi.String(ipv4_cidr),
					},
					Ipv6CidrBlocks: pulumi.StringArray{
						pulumi.String(ipv6_cidr),
					},
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol: pulumi.String("TCP"),
					ToPort:   pulumi.Int(8080),
					FromPort: pulumi.Int(8080),
					CidrBlocks: pulumi.StringArray{
						pulumi.String(ipv4_cidr),
					},
					Ipv6CidrBlocks: pulumi.StringArray{
						pulumi.String(ipv6_cidr),
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// lookup ami
		ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
			Filters: []ec2.GetAmiFilter{
				ec2.GetAmiFilter{
					Name:   "name",
					Values: []string{"csye-6225-*"},
				},
				ec2.GetAmiFilter{
					Name:   "virtualization-type",
					Values: []string{"hvm"},
				},
				ec2.GetAmiFilter{
					Name:   "root-device-type",
					Values: []string{"ebs"},
				},
			},
			Owners:     []string{dev_account_id},
			MostRecent: pulumi.BoolRef(true),
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
			Ami:                   pulumi.String(ami.Id),
			DisableApiTermination: pulumi.Bool(false),
			Tags:                  tags,
		})
		if err != nil {
			return err
		}

		return nil
	})
}
