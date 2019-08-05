# Attunity Enterprise Manager Exporter
## Building
This uses Go Modules to handle dependencies. As long as you have Go v1.11+ then it should build fine.
```bash
go build .
```

## Run Tests
Before submitting a PR, make sure all tests pass still. You'll need to update the password in `config.good.yml` first.

```bash
go test ./collector
```

## Usage
```
./attunity_exporter --help
usage: attunity_exporter [<flags>]

Flags:
  --help                      Show context-sensitive help (also try --help-long and --help-man).
  --http2                     Enable HTTP/2 support
  --port="9103"               the port to bind the exporter to.
  --config-file="config.yml"  path the config.yml file
  ```

## Configuration
This uses a YAML-based configuration file. You can see examples in the `config.yml.example` file and in the `testdata/config.good.yml` file.

### Required fields
| Name | Value |
| ---  |  ---  | 
| user | Username for login. Format: `<user>@<domain>` |
| password | Password for login |
| weblink | Full web-link to Attunity Enterprise Manager |

### Optional fields
| Name | Value |
| ---  |  ---  | 
| timeout | Timeout to set on HTTP client for all requests. |
| included_tasks | List of servers and associated tasks to include. All other tasks will be excluded. |
| excluded_tasks | List of servers and associated tasks to exclude. Takes precedence over `included_tasks`. |