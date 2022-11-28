package mirrosa

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.uber.org/zap"
)

const vpceServiceDescription = "A PrivateLink ROSA cluster has a VPC Endpoint Service which allows Hive to connect" +
	" to the cluster over AWS' internal network (PrivateLink), used for things like backplane and SyncSets."

var _ Component = &VpcEndpointService{}

type VpcEndpointService struct {
	log         *zap.SugaredLogger
	InfraName   string
	PrivateLink bool

	Ec2Client Ec2AwsApi
}

func (c *Client) NewVpcEndpointService() VpcEndpointService {
	return VpcEndpointService{
		log:         c.log,
		InfraName:   c.ClusterInfo.InfraName,
		PrivateLink: c.Cluster.AWS().PrivateLink(),
		Ec2Client:   ec2.NewFromConfig(c.AwsConfig),
	}
}

func (v VpcEndpointService) Validate(ctx context.Context) error {
	// non-PrivateLink clusters do not have a VPC Endpoint Service
	if !v.PrivateLink {
		return nil
	}

	v.log.Info("searching for VPC Endpoint Service")
	var serviceId string
	resp, err := v.Ec2Client.DescribeVpcEndpointServices(ctx, &ec2.DescribeVpcEndpointServicesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{fmt.Sprintf("%s-vpc-endpoint-service", v.InfraName)},
			},
			{
				Name:   aws.String("tag:hive.openshift.io/private-link-access-for"),
				Values: []string{v.InfraName},
			},
		},
	})
	if err != nil {
		return err
	}

	switch len(resp.ServiceDetails) {
	case 0:
		return errors.New("no VPC Endpoint Services found for PrivateLink cluster")
	case 1:
		v.log.Infof("found VPC Endpoint Service: %s", *resp.ServiceDetails[0].ServiceId)
		serviceId = *resp.ServiceDetails[0].ServiceId
	default:
		return errors.New("multiple VPC Endpoint Services found for PrivateLink cluster")
	}

	v.log.Infof("validating VPC Endpoint Service: %s", *resp.ServiceDetails[0].ServiceId)
	cxResp, err := v.Ec2Client.DescribeVpcEndpointConnections(ctx, &ec2.DescribeVpcEndpointConnectionsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("service-id"),
				Values: []string{serviceId},
			},
			{
				Name:   aws.String("vpc-endpoint-state"),
				Values: []string{"available"},
			},
		},
	})
	if err != nil {
		return err
	}

	switch len(cxResp.VpcEndpointConnections) {
	case 0:
		return fmt.Errorf("no available VPC Endpoint connections found for %s", serviceId)
	case 1:
		v.log.Infof("found accepted VPC Endpoint connection for %s", serviceId)
		return nil
	default:
		return fmt.Errorf("multiple available VPC Endpoint connections found for %s", serviceId)
	}
}

func (v VpcEndpointService) Documentation() string {
	return vpceServiceDescription
}

func (v VpcEndpointService) FilterValue() string {
	return "VPC Endpoint Service"
}
