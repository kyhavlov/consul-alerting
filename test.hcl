consul_address = "localhost:8500"

service "redis" {
  change_threshold = 10
  distinct_tags = true
}

service "nginx" {
  change_threshold = 30
}