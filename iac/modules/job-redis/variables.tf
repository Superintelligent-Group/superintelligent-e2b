variable "node_pool" {
  type = string
}

variable "port_number" {
  type = number
}

variable "port_name" {
  type = string
}

variable "cpu" {
  type    = number
  default = 1000
}

variable "memory_mb" {
  type    = number
  default = 2048
}
