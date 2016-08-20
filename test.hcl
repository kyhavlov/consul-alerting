consul_address = "linux-server:8500"
dev_mode = true
change_threshold = 30
log_level = "info"

service "redis" {
  change_threshold = 15
  distinct_tags = true
}

service "nginx" {
  change_threshold = 5
}

service "elasticsearch" {
  distinct_tags = true
  ignored_tags = ["seed", "node"]
}

service "influx" {
  distinct_tags = true
}

handlers {
  stdout {
    enabled = true
  }
}