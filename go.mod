module github.com/hazyhaar/chrc

go 1.24.7

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/go-rod/rod v0.116.2
	github.com/go-rod/stealth v0.4.9
	github.com/hazyhaar/horosvec v0.0.0-20260224091408-6993d04099a2
	github.com/hazyhaar/pkg v0.0.0-20260224091357-ba355365ef24
	github.com/hazyhaar/usertenant v0.0.0-20260224091409-7a3cfce1292d
	github.com/modelcontextprotocol/go-sdk v1.3.1
	golang.org/x/crypto v0.43.0
	golang.org/x/net v0.46.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.46.1
)

replace github.com/hazyhaar/usertenant => ../usertenant

require (
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/quic-go/quic-go v0.59.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.3 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/ysmood/fetchup v0.2.3 // indirect
	github.com/ysmood/goob v0.4.0 // indirect
	github.com/ysmood/got v0.40.0 // indirect
	github.com/ysmood/gson v0.7.3 // indirect
	github.com/ysmood/leakless v0.9.0 // indirect
	golang.org/x/exp v0.0.0-20251023183803-a4bb9ffd2546 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	modernc.org/libc v1.67.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
