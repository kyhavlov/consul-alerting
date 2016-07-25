consul_address = "linux-server:8500"
dev_mode = true
change_threshold = 30

service "redis" {
  change_threshold = 15
  distinct_tags = true
}

service "nginx" {
  change_threshold = 10
}