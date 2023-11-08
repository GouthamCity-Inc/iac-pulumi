package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/c-robinson/iplib"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/route53"
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
		ec2_instance_type := config.Require("ec2-instance-type")
		db_engine := config.Require("db-engine-name")
		db_family := config.Require("db-family")
		db_engine_version := config.Require("db-engine-version")
		db_instance_class := config.Require("db-instance-class")
		db_name := config.Require("db-name")
		db_storage_size := config.RequireInt("db-storage-size")
		db_master_user := config.Require("db-master-user")
		db_master_password := config.Require("db-master-password")
		domain_name := config.Require("domain-name")

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
		assignmentTag := pulumi.String("Assign-6")

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
		webappSg, err := ec2.NewSecurityGroup(ctx, "application-security-group", &ec2.SecurityGroupArgs{
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

		dbSecurityGroup, err := ec2.NewSecurityGroup(ctx, "database-security-group", &ec2.SecurityGroupArgs{
			VpcId: vpc.ID(),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					SecurityGroups: pulumi.StringArray{
						webappSg.ID(),
					},
					Protocol: pulumi.String("tcp"),
					FromPort: pulumi.Int(3306),
					ToPort:   pulumi.Int(3306),
				},
			},
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("database-security-group"),
			},
		})
		if err != nil {
			return err
		}

		_, err = ec2.NewSecurityGroupRule(ctx, "application-security-group-egress-rule", &ec2.SecurityGroupRuleArgs{
			FromPort:              pulumi.Int(3306),
			ToPort:                pulumi.Int(3306),
			Protocol:              pulumi.String("tcp"),
			Type:                  pulumi.String("egress"),
			SourceSecurityGroupId: dbSecurityGroup.ID(),
			SecurityGroupId:       webappSg.ID(),
		})
		if err != nil {
			return err
		}

		// create a string array to store the subnet ids for the db subnet group
		var subnetIds pulumi.StringArray
		for i := range privateSubnets {
			subnetIds = append(subnetIds, privateSubnets[i].ID())
		}

		dbSubnetGroup, err := rds.NewSubnetGroup(ctx, "db-subnet-group", &rds.SubnetGroupArgs{
			SubnetIds: subnetIds,
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("db-subnet-group"),
			},
		})
		if err != nil {
			return err
		}

		dbParamGroup, err := rds.NewParameterGroup(ctx, "param-group", &rds.ParameterGroupArgs{
			Family: pulumi.String(db_family),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("db-parameter-group"),
			},
		})
		if err != nil {
			return err
		}

		policyString, err := json.Marshal(map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Action": "sts:AssumeRole",
					"Effect": "Allow",
					"Sid":    "",
					"Principal": map[string]interface{}{
						"Service": "ec2.amazonaws.com",
					},
				},
			},
		})
		if err != nil {
			return err
		}
		defaultPolicy := string(policyString)

		// Create a new IAM role
		role, err := iam.NewRole(ctx, "cloudwatch-agent-role", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(defaultPolicy),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("cloudwatch-agent-role"),
			},
		})
		if err != nil {
			return err
		}

		// Create a new IAM instance profile
		instanceProfile, err := iam.NewInstanceProfile(ctx, "cloudwatch-instance-profile", &iam.InstanceProfileArgs{
			Role: role.Name,
			Tags: pulumi.StringMap{
				"Name": pulumi.String("cloudwatch-instance-profile"),
			},
		})
		if err != nil {
			return err
		}

		// Attach the policy to the cloudwatch role
		_, err = iam.NewRolePolicyAttachment(ctx, "cloudwatch-agent-policy", &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"),
		})
		if err != nil {
			return err
		}

		db, err := rds.NewInstance(ctx, "db", &rds.InstanceArgs{
			AllocatedStorage:    pulumi.Int(db_storage_size),
			Engine:              pulumi.String(db_engine),
			EngineVersion:       pulumi.String(db_engine_version),
			InstanceClass:       pulumi.String(db_instance_class),
			DbName:              pulumi.String(db_name),
			Username:            pulumi.String(db_master_user),
			Password:            pulumi.String(db_master_password),
			MultiAz:             pulumi.Bool(false),
			PubliclyAccessible:  pulumi.Bool(false),
			DbSubnetGroupName:   dbSubnetGroup.Name,
			ParameterGroupName:  dbParamGroup.Name,
			VpcSecurityGroupIds: pulumi.StringArray{dbSecurityGroup.ID()},
			SkipFinalSnapshot:   pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("csye6225"),
			},
		})
		if err != nil {
			return err
		}

		userData := `#!/bin/bash
{
	echo "spring.jpa.hibernate.ddl-auto=create-drop"
	echo "spring.datasource.url=jdbc:mariadb://${HOST}/${DB_NAME}"
	echo "spring.datasource.username=${DB_USER}"
	echo "spring.datasource.password=${DB_PASSWORD}"
	echo "spring.datasource.driver-class-name=org.mariadb.jdbc.Driver"
	echo "spring.jpa.show-sql:true"
	echo "spring.jpa.properties.hibernate.dialect=org.hibernate.dialect.MariaDBDialect"
	echo "application.config.csv-file=\${USERS_CSV:users.csv}"
	echo "logging.level.org.springframework.security=info"
} >> /opt/csye6225/application.properties
sudo chown csye6225:csye6225 /opt/csye6225/application.properties
sudo chmod 640 /opt/csye6225/application.properties
{
	sudo /opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
		-a fetch-config \
		-m ec2 \
		-c file:/opt/aws/amazon-cloudwatch-agent/etc/cloudwatch-config.json \
		-s
}
`

		userData = strings.Replace(userData, "${DB_NAME}", db_name, -1)
		userData = strings.Replace(userData, "${DB_USER}", db_master_user, -1)
		userData = strings.Replace(userData, "${DB_PASSWORD}", db_master_password, -1)

		// spin up an EC2 instance
		app_server, err := ec2.NewInstance(ctx, "webapp", &ec2.InstanceArgs{
			InstanceType:          pulumi.String(ec2_instance_type),
			VpcSecurityGroupIds:   pulumi.StringArray{webappSg.ID()},
			SubnetId:              publicSubnets[0].ID(),
			KeyName:               pulumi.String(ssh_key),
			Ami:                   pulumi.String(ami_id),
			DisableApiTermination: pulumi.Bool(false),
			IamInstanceProfile:    instanceProfile.ID(),
			UserData: db.Endpoint.ApplyT(
				func(args interface{}) (string, error) {
					endpoint := args.(string)
					userData = strings.Replace(userData, "${HOST}", endpoint, -1)
					return userData, nil
				},
			).(pulumi.StringOutput),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("webapp"),
			},
		}, pulumi.DependsOn([]pulumi.Resource{db}))
		if err != nil {
			return err
		}

		_, err = ec2.NewSecurityGroupRule(ctx, "application-security-group-port-egress-rule", &ec2.SecurityGroupRuleArgs{
			Type:            pulumi.String("egress"),
			FromPort:        pulumi.Int(443),
			ToPort:          pulumi.Int(443),
			Protocol:        pulumi.String("tcp"),
			SecurityGroupId: webappSg.ID(),
			CidrBlocks:      pulumi.StringArray{pulumi.String(ipv4_cidr)},
			Ipv6CidrBlocks:  pulumi.StringArray{pulumi.String(ipv6_cidr)},
		})
		if err != nil {
			return err
		}

		zoneID, err := route53.LookupZone(ctx, &route53.LookupZoneArgs{
			Name: pulumi.StringRef(domain_name),
		}, nil)

		if err != nil {
			return err
		}
		// Create a new A Record for the ec2 instance
		_, err = route53.NewRecord(ctx, "New-A-record", &route53.RecordArgs{
			Name:           pulumi.String(domain_name),
			Type:           pulumi.String("A"),
			Ttl:            pulumi.Int(60),
			ZoneId:         pulumi.String(zoneID.Id),
			Records:        pulumi.StringArray{app_server.PublicIp},
			AllowOverwrite: pulumi.Bool(true),
		}, pulumi.DependsOn([]pulumi.Resource{app_server}))
		if err != nil {
			return err
		}

		ctx.Export("DB Endpoint", db.Endpoint)

		return nil
	})
}
