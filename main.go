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

		tags := pulumi.StringMap{"course": pulumi.String("CSYE-6225"), "assign": pulumi.String("assign-4")}

		vpc, err := ec2.NewVpc(ctx, "myVPC", &ec2.VpcArgs{
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

				publicSubnet, err := ec2.NewSubnet(ctx, "publicSubnet-"+fmt.Sprintf("%d", i), &ec2.SubnetArgs{
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

				privateSubnet, error := ec2.NewSubnet(ctx, "privateSubnet-"+fmt.Sprintf("%d", i), &ec2.SubnetArgs{
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

		igw, err := ec2.NewInternetGateway(ctx, "myGw", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags:  tags,
		})
		if err != nil {
			return err
		}

		publicRouteTable, err := ec2.NewRouteTable(ctx, "publicRouteTable", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags:  tags,
		})
		if err != nil {
			return err
		}

		privateRouteTable, err := ec2.NewRouteTable(ctx, "privateRouteTable", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags:  tags,
		})
		if err != nil {
			return err
		}

		for i := range available.Names {
			if i < 3 {
				_, err := ec2.NewRouteTableAssociation(ctx, "publicRTa-"+fmt.Sprintf("%d", i), &ec2.RouteTableAssociationArgs{
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
				_, err := ec2.NewRouteTableAssociation(ctx, "privateRTa-"+fmt.Sprintf("%d", i), &ec2.RouteTableAssociationArgs{
					SubnetId:     privateSubnets[i].ID(),
					RouteTableId: privateRouteTable.ID(),
				})
				if err != nil {
					return err
				}
			}
		}

		_, err = ec2.NewRoute(ctx, "publicRoute", &ec2.RouteArgs{
			RouteTableId:         publicRouteTable.ID(),
			DestinationCidrBlock: pulumi.String(igw_route),
			GatewayId:            igw.ID(),
		})
		if err != nil {
			return err
		}
		return nil
	})
}
