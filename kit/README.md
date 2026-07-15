# kit — shared device + drawing layer

The best-of interaction/drawing infra for kobo-pkm native apps, extracted from
`app/` and `experiments/riddle/`. A new experiment implements `kit.Handler` and
calls `kit.Run`; the kit owns the whole Nickel-takeover lifecycle.

See the repo-root **`CLAUDE.md`** for the full tour and the golden rules. Files:

| file | build | what |
|------|-------|------|
| `display.go` | all | `Display`, `RefreshMode`, `Touch`/`TouchKind` |
| `draw.go` `bbox.go` | all | mono primitives, dirty-region `BBox` |
| `ink.go` | all | pen-stroke capture / erase / dissolve |
| `text.go` | all | legible Go-font faces + word layout |
| `config.go` | all | device/calibration/sleep keys + `Extra` map |
| `fb_linux.go` | device | hwtcon framebuffer + waveforms |
| `input_linux.go` | device | evdev pen/touch/keys, frontlight |
| `power_linux.go` | device | the MTK suspend ritual |
| `app_linux.go` | device | `Run`, `Handler`, `Runtime` — the main loop |
| `sim.go` | `-tags sim` | PNG simulator backend |
| `scripts/*.tmpl` | — | Nickel-takeover run/restart (deploy substitutes `@APP@`) |

```go
kit.Run(cfg, base, func(rt *kit.Runtime) kit.Handler {
    return &myApp{rt: rt, /* … */}
})
```

Build checks:

```
go build -tags sim ./...                                   # portable + sim
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build ./...  # device
```
