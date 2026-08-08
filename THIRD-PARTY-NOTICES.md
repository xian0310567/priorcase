# 제3자 라이선스 고지 / Third-Party Notices

casebook 바이너리에는 아래 오픈소스 구성요소가 포함된다. 각 구성요소는 자체
라이선스를 따르며, 그 라이선스 전문은 각 프로젝트 저장소에 있다.

casebook binaries include the open-source components listed below. Each is
governed by its own license; full texts are available in the respective
project repositories.

| 구성요소 / Component | 버전 / Version | 라이선스 / License |
| --- | --- | --- |
| `github.com/fsnotify/fsnotify` | v1.10.1 | BSD-3-Clause |
| `github.com/gofrs/flock` | v0.12.1 | BSD-3-Clause |
| `github.com/google/jsonschema-go` | v0.4.3 | MIT |
| `github.com/modelcontextprotocol/go-sdk` | v1.7.0 | MIT |
| `github.com/pelletier/go-toml/v2` | v2.4.3 | MIT |
| `github.com/segmentio/asm` | v1.1.3 | MIT |
| `github.com/segmentio/encoding` | v0.5.4 | MIT |
| `github.com/spf13/cobra` | v1.10.2 | Apache-2.0 |
| `github.com/spf13/pflag` | v1.0.9 | BSD-3-Clause |
| `github.com/yosida95/uritemplate/v3` | v3.0.2 | BSD-3-Clause |
| `go.yaml.in/yaml/v3` | v3.0.5 | MIT |
| `golang.org/x/oauth2` | v0.35.0 | BSD-3-Clause |
| `golang.org/x/sync` | v0.20.0 | BSD-3-Clause |
| `golang.org/x/text` | v0.28.0 | BSD-3-Clause |
| `golang.org/x/time` | v0.15.0 | BSD-3-Clause |
| `golang.org/x/sys` | v0.41.0 | BSD-3-Clause |

Go 표준 라이브러리는 BSD-3-Clause 를 따른다 (https://go.dev/LICENSE).
The Go standard library is BSD-3-Clause licensed.

이 목록은 `go list -deps ./cmd/cb` 로 **실제 바이너리에 링크되는 것만** 추렸다.
테스트 전용 의존성(testify, go-cmp 등)은 배포물에 들어가지 않아 제외했다.
