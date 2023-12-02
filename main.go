package main

import (
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/c-robinson/iplib"
	"github.com/google/uuid"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/acm"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/alb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/autoscaling"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/route53"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sns"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/storage"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		var ports []int
		var alb_ports []int
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
		deployment_path := config.Require("lambda-deployment-path")
		lambda_handler := config.Require("lambda-handler")
		gcp_project_id := config.Require("gcp-project-id")
		gcp_cloud_storage_role := config.Require("gcp-cloud-storage-role")
		smtp_host := config.Require("smtp-host")
		smtp_port := config.Require("smtp-port")
		smtp_username := config.Require("smtp-user")
		smtp_password := config.Require("smtp-password")
		sender_email := config.Require("sender-email")

		config.RequireObject("ports", &ports)
		config.RequireObject("alb-ports", &alb_ports)

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

		// ALB security group ingress rules
		var albSgIngressRules ec2.SecurityGroupIngressArray
		for i := range alb_ports {
			albSgIngressRules = append(albSgIngressRules, &ec2.SecurityGroupIngressArgs{
				Protocol: pulumi.String("TCP"),
				ToPort:   pulumi.Int(alb_ports[i]),
				FromPort: pulumi.Int(alb_ports[i]),
				CidrBlocks: pulumi.StringArray{
					pulumi.String(ipv4_cidr),
				},
				Ipv6CidrBlocks: pulumi.StringArray{
					pulumi.String(ipv6_cidr),
				},
			})
		}

		// setup ALB security group
		albSg, err := ec2.NewSecurityGroup(ctx, "load-balancer-security-group", &ec2.SecurityGroupArgs{
			VpcId:       vpc.ID(),
			Description: pulumi.String("load balancer security group"),
			Ingress:     albSgIngressRules,
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("load-balancer-security-group"),
			},
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
				SecurityGroups: pulumi.StringArray{
					albSg.ID(),
				},
			})
		}

		// setup ASG security group
		webappSg, err := ec2.NewSecurityGroup(ctx, "application-security-group", &ec2.SecurityGroupArgs{
			VpcId:       vpc.ID(),
			Description: pulumi.String("application security group"),
			Ingress:     sgIngressRules,
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("application-security-group"),
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

		// security group rule for ALB to ASG healthcheck
		_, err = ec2.NewSecurityGroupRule(ctx, "alb-to-asg-healthcheck-egress", &ec2.SecurityGroupRuleArgs{
			FromPort:              pulumi.Int(8080),
			ToPort:                pulumi.Int(8080),
			Protocol:              pulumi.String("tcp"),
			Type:                  pulumi.String("egress"),
			SourceSecurityGroupId: webappSg.ID(),
			SecurityGroupId:       albSg.ID(),
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

		_, err = iam.NewRolePolicyAttachment(ctx, "sns-publish-policy", &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonSNSFullAccess"),
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

		// define the launch template
		launchTemplate, err := ec2.NewLaunchTemplate(ctx, "webapp-launch-template", &ec2.LaunchTemplateArgs{
			Name:                  pulumi.String("webapp-launch-template"),
			ImageId:               pulumi.String(ami_id),
			InstanceType:          pulumi.String(ec2_instance_type),
			KeyName:               pulumi.String(ssh_key),
			DisableApiTermination: pulumi.Bool(false),
			IamInstanceProfile: &ec2.LaunchTemplateIamInstanceProfileArgs{
				Name: instanceProfile.Name,
			},
			VpcSecurityGroupIds: pulumi.StringArray{webappSg.ID()},
			UserData: db.Endpoint.ApplyT(
				func(args interface{}) (string, error) {
					endpoint := args.(string)
					userData = strings.Replace(userData, "${HOST}", endpoint, -1)
					return b64.StdEncoding.EncodeToString([]byte(userData)), nil
				},
			).(pulumi.StringOutput),
		})
		if err != nil {
			return err
		}

		// Create a Target Group for our Autoscaling Group
		targetGroup, err := alb.NewTargetGroup(ctx, "alb-target-group", &alb.TargetGroupArgs{
			Port:       pulumi.Int(8080),
			Protocol:   pulumi.String("HTTP"),
			TargetType: pulumi.String("instance"),
			VpcId:      vpc.ID(),
			HealthCheck: &alb.TargetGroupHealthCheckArgs{
				HealthyThreshold:   pulumi.Int(2),
				Interval:           pulumi.Int(6),
				Path:               pulumi.String("/healthz"),
				Port:               pulumi.String("8080"),
				Protocol:           pulumi.String("HTTP"),
				Timeout:            pulumi.Int(5),
				UnhealthyThreshold: pulumi.Int(3),
			},
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("alb-target-group"),
			},
		})
		if err != nil {
			return err
		}

		publicSubnetIDs := pulumi.StringArray{}
		for _, subnet := range publicSubnets {
			publicSubnetIDs = append(publicSubnetIDs, subnet.ID())
		}

		// define the autoscaling group
		asg, err := autoscaling.NewGroup(ctx, "auto-scaling-group", &autoscaling.GroupArgs{
			Name:               pulumi.String("webapp-auto-scaling-group"),
			DesiredCapacity:    pulumi.Int(1),
			MaxSize:            pulumi.Int(3),
			MinSize:            pulumi.Int(1),
			DefaultCooldown:    pulumi.Int(60),
			VpcZoneIdentifiers: publicSubnetIDs,
			TargetGroupArns: pulumi.StringArray{
				targetGroup.Arn,
			},
			LaunchTemplate: &autoscaling.GroupLaunchTemplateArgs{
				Id:      launchTemplate.ID(),
				Version: pulumi.String("$Latest"),
			},
		})
		if err != nil {
			return err
		}

		// Create a tag and attach it to the AutoScaling Group
		_, _ = autoscaling.NewTag(ctx, "auto-scaling-group-tag", &autoscaling.TagArgs{
			AutoscalingGroupName: asg.Name, // reference to the previously created AutoScalingGroup
			Tag: autoscaling.TagTagArgs{
				Key:               pulumi.String("Name"),
				Value:             pulumi.String("webapp"),
				PropagateAtLaunch: pulumi.Bool(true),
			},
		})

		// scale up policy
		scaleUpPolicy, err := autoscaling.NewPolicy(ctx, "scale-up-policy", &autoscaling.PolicyArgs{
			ScalingAdjustment:     pulumi.Int(1),
			AdjustmentType:        pulumi.String("ChangeInCapacity"),
			MetricAggregationType: pulumi.String("Average"),
			Cooldown:              pulumi.Int(60),
			AutoscalingGroupName:  asg.Name,
		})
		if err != nil {
			return err
		}
		scaleDownPolicy, err := autoscaling.NewPolicy(ctx, "scale-down-policy", &autoscaling.PolicyArgs{
			ScalingAdjustment:     pulumi.Int(-1),
			AdjustmentType:        pulumi.String("ChangeInCapacity"),
			MetricAggregationType: pulumi.String("Average"),
			Cooldown:              pulumi.Int(60),
			AutoscalingGroupName:  asg.Name,
		})
		if err != nil {
			return err
		}

		_, _ = cloudwatch.NewMetricAlarm(ctx, "scale-up-alarm", &cloudwatch.MetricAlarmArgs{
			AlarmActions:       pulumi.Array{scaleUpPolicy.Arn},
			Dimensions:         pulumi.StringMap{"AutoScalingGroupName": asg.Name},
			AlarmDescription:   pulumi.String("This metric triggers when the CPU usage exceeds 5%"),
			ComparisonOperator: pulumi.String("GreaterThanOrEqualToThreshold"),
			EvaluationPeriods:  pulumi.Int(2),
			MetricName:         pulumi.String("CPUUtilization"),
			Namespace:          pulumi.String("AWS/EC2"),
			Period:             pulumi.Int(60),
			Statistic:          pulumi.String("Average"),
			Threshold:          pulumi.Float64(5.0),
			Tags:               pulumi.StringMap{"Name": pulumi.String("scale-up-alarm")},
		})

		_, _ = cloudwatch.NewMetricAlarm(ctx, "scale-down-alarm", &cloudwatch.MetricAlarmArgs{
			AlarmActions:       pulumi.Array{scaleDownPolicy.Arn},
			Dimensions:         pulumi.StringMap{"AutoScalingGroupName": asg.Name},
			AlarmDescription:   pulumi.String("This metric triggers when the CPU usage goes below 3%"),
			ComparisonOperator: pulumi.String("LessThanOrEqualToThreshold"),
			EvaluationPeriods:  pulumi.Int(2),
			MetricName:         pulumi.String("CPUUtilization"),
			Namespace:          pulumi.String("AWS/EC2"),
			Period:             pulumi.Int(60),
			Statistic:          pulumi.String("Average"),
			Threshold:          pulumi.Float64(3.0),
			Tags:               pulumi.StringMap{"Name": pulumi.String("scale-down-alarm")},
		})

		loadBalancer, err := lb.NewLoadBalancer(ctx, "load-balancer", &lb.LoadBalancerArgs{
			Internal:         pulumi.Bool(false),
			LoadBalancerType: pulumi.String("application"),
			SecurityGroups: pulumi.StringArray{
				albSg.ID(),
			},
			Subnets: publicSubnetIDs,
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("load-balancer"),
			},
		})
		if err != nil {
			return err
		}


		ssl_certificate, err := acm.LookupCertificate(ctx, &acm.LookupCertificateArgs{
			Domain: domain_name,
			Statuses: []string{
				"ISSUED",
			},
		}, nil)
		if err != nil {
			return err
		}

		_, err = alb.NewListener(ctx, "HTTPS listener", &alb.ListenerArgs{
			DefaultActions: alb.ListenerDefaultActionArray{
				alb.ListenerDefaultActionArgs{
					Type:           pulumi.String("forward"),
					TargetGroupArn: targetGroup.Arn,
				},
			},
			LoadBalancerArn: loadBalancer.Arn,
			CertificateArn:  pulumi.String(ssl_certificate.Arn),
			Port:            pulumi.Int(443),
			Protocol:        pulumi.String("HTTPS"),
		}, pulumi.DependsOn([]pulumi.Resource{loadBalancer}))
		if err != nil {
			return err
		}

		zoneID, err := route53.LookupZone(ctx, &route53.LookupZoneArgs{
			Name: pulumi.StringRef(domain_name),
		}, nil)

		if err != nil {
			return err
		}
		// Create a new A Record for the load balancer
		_, err = route53.NewRecord(ctx, "New-A-record", &route53.RecordArgs{
			Name:   pulumi.String(domain_name),
			Type:   pulumi.String("A"),
			ZoneId: pulumi.String(zoneID.Id),
			Aliases: route53.RecordAliasArray{
				&route53.RecordAliasArgs{
					Name:                 loadBalancer.DnsName,
					ZoneId:               loadBalancer.ZoneId,
					EvaluateTargetHealth: pulumi.Bool(true),
				},
			},
			AllowOverwrite: pulumi.Bool(true),
		}, pulumi.DependsOn([]pulumi.Resource{asg}))
		if err != nil {
			return err
		}

		service_account, err := serviceaccount.NewAccount(ctx, "aws-lambda-service-account", &serviceaccount.AccountArgs{
			AccountId:   pulumi.String("aws-lambda-service-account"),
			DisplayName: pulumi.String("aws-lambda-service-account"),
			Project:     pulumi.String(gcp_project_id),
		})
		if err != nil {
			return err
		}

		sa_access_key, err := serviceaccount.NewKey(ctx, "service-account-access-key", &serviceaccount.KeyArgs{
			ServiceAccountId: service_account.Name,
			PublicKeyType:    pulumi.String("TYPE_X509_PEM_FILE"),
		})
		if err != nil {
			return err
		}

		newUUID := uuid.New()
		uuidStr := newUUID.String()

		gcp_bucket, err := storage.NewBucket(ctx, "gcp-bucket", &storage.BucketArgs{
			Project:                  pulumi.String(gcp_project_id),
			Name:                     pulumi.String("csyebucket" + uuidStr),
			PublicAccessPrevention:   pulumi.String("enforced"),
			Location:                 pulumi.String("US"),
			StorageClass:             pulumi.String("STANDARD"),
			ForceDestroy:             pulumi.Bool(true),
			UniformBucketLevelAccess: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

		_, err = storage.NewBucketIAMBinding(ctx, "bucket-iam", &storage.BucketIAMBindingArgs{
			Bucket: gcp_bucket.Name,
			Role:   pulumi.String(gcp_cloud_storage_role),
			Members: pulumi.StringArray{
				service_account.Email.ApplyT(func(args interface{}) (string, error) {
					email := args.(string)
					return "serviceAccount:" + email, nil
				}).(pulumi.StringOutput),
			},
		}, pulumi.DependsOn([]pulumi.Resource{service_account, gcp_bucket}))
		if err != nil {
			return err

		}

		sns_topic, err := sns.NewTopic(ctx, "csye6225-submissions", &sns.TopicArgs{
			Name: pulumi.String("csye6225-submissions"),
		})
		if err != nil {
			return err
		}

		lambda_role, _ := iam.NewRole(ctx, "lambda-role", &iam.RoleArgs{
			Name: pulumi.String("lambda-role"),
			AssumeRolePolicy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [
					{
						"Action": "sts:AssumeRole",
						"Principal": {
							"Service": "lambda.amazonaws.com"
							
					},
						"Effect": "Allow",
						"Sid": ""
					}
				]
			}`),
		})

		lambda_loggroup, err := cloudwatch.NewLogGroup(ctx, "lambda-log-group", &cloudwatch.LogGroupArgs{
			RetentionInDays: pulumi.Int(14),
			Name:            pulumi.String("lambda-log-group"),
		})
		if err != nil {
			return err
		}

		lambda_logging_policy_document, err := iam.GetPolicyDocument(ctx, &iam.GetPolicyDocumentArgs{
			Statements: []iam.GetPolicyDocumentStatement{
				{
					Effect: pulumi.StringRef("Allow"),
					Actions: []string{
						"logs:CreateLogGroup",
						"logs:CreateLogStream",
						"logs:PutLogEvents",
					},
					Resources: []string{
						"arn:aws:logs:*:*:*",
					},
				},
			},
		}, nil)
		if err != nil {
			return err
		}

		lambda_logging_policy, err := iam.NewPolicy(ctx, "lambda-logging-policy", &iam.PolicyArgs{
			Path:        pulumi.String("/"),
			Description: pulumi.String("IAM policy for logging from a lambda"),
			Policy:      pulumi.String(lambda_logging_policy_document.Json),
		})
		if err != nil {
			return err
		}

		lambda_logs, err := iam.NewRolePolicyAttachment(ctx, "lambdaLogs", &iam.RolePolicyAttachmentArgs{
			Role:      lambda_role.Name,
			PolicyArn: lambda_logging_policy.Arn,
		}, pulumi.DependsOn([]pulumi.Resource{
			lambda_role,
			lambda_logging_policy,
		}))

		if err != nil {
			return err
		}

		dynamodb_policy_document, err := iam.GetPolicyDocument(ctx, &iam.GetPolicyDocumentArgs{
			Statements: []iam.GetPolicyDocumentStatement{
				{
					Effect: pulumi.StringRef("Allow"),
					Actions: []string{
						"dynamodb:GetItem",
						"dynamodb:PutItem",
						"dynamodb:UpdateItem",
						"dynamodb:DeleteItem",
						"dynamodb:Scan",
						"dynamodb:Query",
					},
					Resources: []string{
						"arn:aws:dynamodb:*:*:table/*",
					},
				},
			},
		}, nil)
		if err != nil {
			return err
		}

		dynamodb_policy, err := iam.NewPolicy(ctx, "dynamodb-policy", &iam.PolicyArgs{
			Path:        pulumi.String("/"),
			Description: pulumi.String("IAM policy for dynamodb"),
			Policy:      pulumi.String(dynamodb_policy_document.Json),
		})
		if err != nil {
			return err
		}

		_, err = iam.NewRolePolicyAttachment(ctx, "dynamodb-policy-attachment", &iam.RolePolicyAttachmentArgs{
			Role:      lambda_role.Name,
			PolicyArn: dynamodb_policy.Arn,
		}, pulumi.DependsOn([]pulumi.Resource{
			lambda_role,
			dynamodb_policy,
		}))
		if err != nil {
			return err
		}

		dynamodb, err := dynamodb.NewTable(ctx, "dynamodb", &dynamodb.TableArgs{
			Name: pulumi.String("csye6225-submissions-table"),
			Attributes: dynamodb.TableAttributeArray{
				&dynamodb.TableAttributeArgs{
					Name: pulumi.String("id"),
					Type: pulumi.String("S"),
				},
			},
			HashKey:       pulumi.String("id"),
			ReadCapacity:  pulumi.Int(5),
			WriteCapacity: pulumi.Int(5),
			Tags: pulumi.StringMap{
				"course": courseTag,
				"assign": assignmentTag,
				"Name":   pulumi.String("dynamodb"),
			},
		})
		if err != nil {
			return err
		}

		lambda_function, err := lambda.NewFunction(ctx, "lambda-function", &lambda.FunctionArgs{
			Name:    pulumi.String("csye-submissions-lambda"),
			Handler: pulumi.String(lambda_handler),
			Role:    lambda_role.Arn,
			Runtime: pulumi.String("python3.11"),
			Code:    pulumi.NewFileArchive(deployment_path),
			Environment: &lambda.FunctionEnvironmentArgs{
				Variables: pulumi.StringMap{
					"BUCKET_NAME":        gcp_bucket.Name,
					"GOOGLE_CREDENTIALS": sa_access_key.PrivateKey,
					"DYNAMODB_TABLE":     dynamodb.Name,
					"SMTP_HOST":          pulumi.String(smtp_host),
					"SMTP_PORT":          pulumi.String(smtp_port),
					"SMTP_USERNAME":      pulumi.String(smtp_username),
					"SMTP_PASSWORD":      pulumi.String(smtp_password),
					"SENDER_EMAIL":       pulumi.String(sender_email),
				},
			},
			Timeout: pulumi.Int(10),
		}, pulumi.DependsOn([]pulumi.Resource{
			lambda_logs,
			lambda_loggroup,
		}))
		if err != nil {
			return err
		}

		_, err = lambda.NewPermission(ctx, "withSns", &lambda.PermissionArgs{
			Action:    pulumi.String("lambda:InvokeFunction"),
			Function:  lambda_function.Name,
			Principal: pulumi.String("sns.amazonaws.com"),
			SourceArn: sns_topic.Arn,
		})
		if err != nil {
			return err
		}

		_, err = sns.NewTopicSubscription(ctx, "lambda-subscription", &sns.TopicSubscriptionArgs{
			Protocol: pulumi.String("lambda"),
			Topic:    sns_topic.Arn,
			Endpoint: lambda_function.Arn,
		})
		if err != nil {
			return err
		}

		ctx.Export("DB Endpoint", db.Endpoint)

		return nil
	})
}
