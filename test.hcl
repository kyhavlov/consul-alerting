consul_address = "linux-server:8500"
dev_mode = true

service "redis" {
  change_threshold = 10
  distinct_tags = true
}

service "nginx" {
  change_threshold = 30
}