Consul Alerting
================
[![Build Status](https://travis-ci.org/kyhavlov/consul-alerting.svg?branch=master)](https://travis-ci.org/kyhavlov/consul-alerting)

This project provides a daemon to run alongside Consul and alert on health check failures. It can be configured to watch only local service and node health checks, or to use the catalog to monitor all services/checks. It distributes the alerting load by acquiring individual locks on the nodes/services it is monitoring, allowing daemons on different hosts to share the work and to pick up monitoring for one another in the event of node failure.

The Consul key/value store is used for storing persistent state about alerts; in the event of a process being restarted or lock ownership changing, the information about the last alert sent for a given service/node is preserved. This is to avoid sending duplicate alerts and leaving hanging alerts that never resolve. Only the most recently known state is held in the KV store for comparisons so that the usage does not increase over time.

Usage
-----

### Discovery Modes

The scope of both the services and nodes to monitor can be configured via the `service_watch` and `node_watch` config parameters respectively. In a small deployment with few services/nodes, global mode can be used for both settings and consul-alerting will attempt to watch all services and nodes in the catalog. For a large deployment with many services and nodes, both can be set to local mode and consul-alerting can be run on every node, monitoring only the services and checks registered with the local Consul agent.

### Command Line
To run the daemon, pass the `-config` flag for the config file location. If a config file is not specified, the default configuration settings will be used and alerts will be logged on the `stdout` handler.

`consul-alerting [--help] -config=/path/to/config.hcl`

### Configuration File(s)
The Consul Alerting configuration files are written in [HashiCorp Configuration Language (HCL)][HCL]. By proxy, this means the Consul Alerting configuration file is JSON-compatible. For more information, please see the [HCL specification][HCL].

##### Example Config
```hcl
consul_address = "localhost:8500"
consul_token = "secret"
datacenter = "prod-1"

node_watch = "local"
service_watch = "global"

change_threshold = 60
default_handlers = ["email.admin", "pagerduty.page_ops"]

log_level = "info"

service "redis" {
  change_threshold = 30
  distinct_tags = true
  ignored_tags = ["master", "node"]
}

service "webapp" {
  change_threshold = 45
  handlers = ["slack.dev_channel"]
}

handler "stdout" "log" {
  log_level = "warn"
}

handler "email" "admin" {
  recipients = ["admin@example.com"]
}

handler "pagerduty" "page_ops" {
  service_key = "asdf1234"
  max_retries = 10
}

handler "slack" "dev_channel" {
  api_token = "mytoken"
  channel_name = "webapp_team"
}
```

#### Global Options

|       Option       | Description |
| ------------------ |------------ |
| `consul_address`   | The address of the Consul agent to connect to. Defaults to `localhost:8500`.
| `consul_token`     | The [Consul API token][Consul ACLs]. There is no default value.
| `datacenter`       | The datacenter name to use in alerts. Defaults to the datacenter of the Consul agent.
| `node_watch`       | The setting to use for discovering nodes. If set to `local`, only the local node's health will be watched. If set to `global`, all nodes in the catalog will be watched. Defaults to `local`.
| `service_watch`    | The setting to use for discovering services. If set to `local`, only services on the local node will be watched. If set to `global`, all services in the catalog will be watched. Defaults to `local`.
| `change_threshold` | The time (in seconds) that a check must be in a failing state before alerting. Defaults to 60.
| `default_handlers` | The default list of handlers to send alerts to, in the form `type.name`. Defaults to all configured handlers.
| `log_level`        | The logging level to use. Defaults to `info`.

#### Service Options
The following options can be specified in a service block:

|       Option       | Description |
| ------------------ |------------ |
| `change_threshold` | The time (in seconds) that this service must be in a failing state before alerting. Defaults to the global `change_threshold`.
| `distinct_tags`    | Treat every tag registered as a distinct service, and specify the tag when sending alerts about the failing service. Defaults to false.
| `ignored_tags`     | Tags to ignore when using `distinct_tags`. Useful when excluding generic tags like "master" that are spread across multiple clusters of the same service.
| `handlers`         | A list of handlers to send alerts for this service, in the form `type.name`. If not specified, the global `default_handlers` setting is used.

#### Handler Options
**stdout**

|       Option       | Description |
| ------------------ |------------ |
| `log_level`        | The level to log alerts on. Defaults to "warn".

**email**

|       Option       | Description |
| ------------------ |------------ |
| `recipients`       | The list of email addresses to use.

**pagerduty**

|       Option       | Description |
| ------------------ |------------ |
| `service_key`      | The PagerDuty api key to use.
| `max_retries`      | The maximum number of times to retry after an api failure when alerting. Defaults to 5.

**slack**

|       Option       | Description |
| ------------------ |------------ |
| `api_token`        | The Slack api token to use.
| `channel_name`     | The Slack channel name to send alerts to.

#### Example log output:
```
[Sep  6 01:42:41]  INFO Loaded handler: stdout.log
[Sep  6 01:42:41]  INFO Using Consul agent at 192.168.1.3:8500
[Sep  6 01:42:41]  INFO Using datacenter: dc1
[Sep  6 01:42:41]  INFO Monitoring local node (consul)'s checks
[Sep  6 01:42:41]  INFO Discovering services from catalog
[Sep  6 01:42:41]  INFO Waiting to acquire lock on node consul...
[Sep  6 01:42:41]  INFO Service found: consul, tags: []
[Sep  6 01:42:41]  INFO Service found: nginx, tags: [gamma delta]
[Sep  6 01:42:41]  INFO Service found: redis, tags: [alpha beta]
[Sep  6 01:42:41]  INFO Waiting to acquire lock on service nginx...
[Sep  6 01:42:41]  INFO Waiting to acquire lock on service redis (tag: beta)...
[Sep  6 01:42:41]  INFO Waiting to acquire lock on service redis (tag: alpha)...
[Sep  6 01:42:41]  INFO Waiting to acquire lock on service consul...
[Sep  6 01:42:41]  INFO Acquired lock for service consul
[Sep  6 01:42:41]  INFO Acquired lock for node consul
[Sep  6 01:42:41]  INFO Acquired lock for service redis (tag: beta)
[Sep  6 01:42:41]  INFO Acquired lock for service nginx
[Sep  6 01:42:41]  INFO Acquired lock for service redis (tag: alpha)
[Sep  6 01:42:47]  WARN dc1: service nginx is now warning
[Sep  6 01:42:47]  WARN Failing checks:
[Sep  6 01:42:47]  WARN => (node) consul
[Sep  6 01:42:47]  WARN ==> (check) Service 'nginx' check:
[Sep  6 01:42:47]  WARN example warning check output
[Sep  6 01:43:02]  WARN dc1: service nginx is now passing
[Sep  6 01:43:31]  WARN dc1: node consul is now warning
[Sep  6 01:43:31]  WARN Failing checks:
[Sep  6 01:43:31]  WARN => (check) memory usage:
[Sep  6 01:43:31]  WARN example warning check output
```

[HCL]: https://github.com/hashicorp/hcl "HashiCorp Configuration Language (HCL)"
[Consul ACLs]: https://www.consul.io/docs/internals/acl.html "Consul ACLs"
