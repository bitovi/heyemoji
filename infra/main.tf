provider "aws" {
  region = var.aws_region
}

resource "aws_instance" "heyemoji_ec2" {
  ami           = var.ami_id
  instance_type = var.instance_type

  tags = {
    Name = "heyemoji-ec2"
  }

  user_data = <<-EOF
    #!/bin/bash
    sudo apt-get update
    sudo apt-get install -y docker.io

    sudo systemctl enable docker
    sudo systemctl start docker

    echo "${container_token}" | sudo docker login ghcr.io -u ${container_user} --password-stdin

    sudo docker pull ${container_image}:${container_tag}
    sudo docker run -d --restart=always --name heyemoji ${container_image}:${container_tag}
    EOF

  # Optional: Give your security group
  vpc_security_group_ids = [aws_security_group.heyemoji_sg.id]
}

resource "aws_security_group" "heyemoji_sg" {
  name        = "heyemoji-sg"
  description = "Allow SSH and HTTP"
  vpc_id      = var.vpc_id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"] # Edit as needed; restrict for real deployments
  }

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
