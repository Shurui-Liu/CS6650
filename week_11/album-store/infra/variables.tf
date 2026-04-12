variable "region" {
  default = "us-east-1"
}

variable "db_password" {
  description = "RDS master password"
  sensitive   = true
}

variable "ec2_instance_profile" {
  description = "Name of the manually-created IAM instance profile"
  default     = "album-store-ec2-role"
}

variable "ec2_key_name" {
  description = "EC2 key pair name for SSH access"
}
