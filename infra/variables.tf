variable "aws_region" {
  default = "us-east-1"
}

variable "ami_id" {
  default = "ami-0c55b159cbfafe1f0" # Ubuntu 22.04 for us-east-1
}

variable "instance_type" {
  default = "t3.micro"
}

variable "vpc_id" {
  description = "Your VPC id"
}

# These can be set via TF_VAR_... environment variables or a tfvars file/secrets:
variable "container_image" {
  description = "Container image to deploy, e.g., ghcr.io/OWNER/REPO"
}

variable "container_tag" {
  default = "latest"
}

variable "container_user" {
  description = "GitHub user for ghcr.io"
}

variable "container_token" {
  description = "Personal Access Token with repo & package read access"
}
