# iac-pulumi

This project automates the creation of essential networking infrastructure in the AWS cloud using Pulumi. It sets up a Virtual Private Cloud (VPC) with subnets, an Internet Gateway, and route tables as described below.

## Infrastructure Setup

### VPC and Subnets
- Creates a Virtual Private Cloud (VPC).
- Sets up three public subnets and three private subnets, with each public-private subnet pair located in different availability zones in the same AWS region within the same VPC.

### Internet Gateway
- Creates an Internet Gateway resource.
- Attaches the Internet Gateway to the VPC, enabling connectivity to the public internet from the public subnets.

### Route Tables
- Creates a public route table.
- Associates all public subnets with the public route table to route traffic to the Internet Gateway.
- Creates a private route table.
- Associates all private subnets with the private route table.

### Default Route
- Adds a default route in the public route table with the destination CIDR block "0.0.0.0/0," directing traffic to the Internet Gateway as the target.

## Usage

### Prerequisites
Before you proceed, ensure you have the following prerequisites installed:
- [Pulumi CLI](https://www.pulumi.com/docs/get-started/install/)

### Deployment
1. Clone this repository:

2. Change into the project directory:


3. Configure your AWS credentials:


4. Initialize the Pulumi stack and bring it up:  
`pulumi stack init dev`  
`pulumi up`


6. Confirm the changes when prompted.

### Destruction
To tear down the created infrastructure, use the following command:

`pulumi destroy`


Follow the on-screen prompts to confirm the destruction of resources.


## License
This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.


