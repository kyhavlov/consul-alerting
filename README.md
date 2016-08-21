Consul Alerting
================

This project provides a daemon to run alongside the local Consul agent and alert on health check failures. It can be run in local mode, where it will only monitor services and checks on the local agent, or global mode, where it will alert for all nodes/services in the catalog. It distributes the alerting load by acquiring individual locks on the nodes/services it is monitoring, allowing daemons on different nodes to share the watches.

### Configuration File(s)
The Consul Alerting configuration files are written in [HashiCorp Configuration Language (HCL)][HCL]. By proxy, this means the Consul Alerting configuration file is JSON-compatible. For more information, please see the [HCL specification][HCL].

##### Example Config
```hcl
consul_address = "localhost:8500"

global_mode = true
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
  ignored_tags = ["master", "client"]
}

service "influx" {
  distinct_tags = true
}

handlers {
  stdout {
    enabled = true
    log_level = "warn"
  }
  email {
    enabled = false
    recipients = ["admin@example.com"]
  }
}
```

#### Global Options

|       Option       | Description |
| ------------------ |------------ |
| `consul_address`   | The address of the consul agent to connect to. Defaults to `localhost:8500`.
| `global_mode`      | Use the catalog to discover services/nodes instead of the local agent. Defaults to false.
| `change_threshold` | The time (in seconds) that a check must be in a failing state before alerting. Defaults to 60.
| `log_level`        | The logging level to use. Defaults to `info`.

#### Service Options
The following options can be specified in a service block:

|       Option       | Description |
| ------------------ |------------ |
| `change_threshold` | The time (in seconds) that this service must be in a failing state before alerting. Defaults to the global `change_threshold`.
| `distinct_tags`    | Treat every tag registered as a distinct service, and specify the tag when sending alerts about the failing service. Defaults to false.
| `ignored_tags`     | Tags to ignore when using `distinct_tags`. Useful when excluding generic tags like "master" that are spread across multiple clusters.

#### Handler Options
Handlers must have `enabled = true` set in order to be active.

**stdout**

|       Option       | Description |
| ------------------ |------------ |
| `log_level`        | The log level to log alerts to. Defaults to "warn".

**email**

|       Option       | Description |
| ------------------ |------------ |
| `recipients`       | A list of email addresses to send alerts to.

[HCL]: https://github.com/hashicorp/hcl "HashiCorp Configuration Language (HCL)"