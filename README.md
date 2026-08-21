# GVL — Govee LAN lights + sleep/wake schedules

`gvl` is a small Go CLI for [Govee](https://govee.com) lights over the **LAN UDP API**.  
`gvld` is a tiny companion daemon that runs sleep/wake ramps on a schedule.

No Govee cloud account required. LAN control must be enabled in the Govee app.

## Install

```bash
go install github.com/Chris-Alexander-Pop/gvl/cmd/gvl@latest
go install github.com/Chris-Alexander-Pop/gvl/cmd/gvld@latest
```

Or from a clone:

```bash
make install
```

## Quick start (direct LAN)

```bash
gvl discover
gvl crawl --save          # unicast subnet probe when multicast misses
gvl on
gvl bright 40
gvl color blue
gvl temp warm
gvl mode aurora
gvl stop          # when using gvld; local modes exit with Ctrl+C
gvl --url local off       # skip the daemon; talk UDP on this machine
```

## Daemon + schedules

Run `gvld` on a machine that can reach the light over UDP (usually the same LAN).

```bash
export GVL_DEVICE_IP=192.0.2.10   # bootstrap / last-known IP (from gvl discover)
export GVL_DISCOVER_SUBNET=192.0.2.0/24  # optional; for DHCP moves across subnets
export GVL_AUTO_DISCOVER=1        # default on; set 0 to disable
export GVL_TOKEN=some-secret
export GVL_TZ=UTC
export GVL_DATA_DIR=./data
gvld
```

If the bulb’s DHCP address changes, `gvld` auto-rediscovers: multicast scan when on the same LAN, otherwise a unicast status probe of `GVL_DISCOVER_SUBNET` (or the /24 of the last IP). The new IP is stored in `$GVL_DATA_DIR/device.json` and reused on restart.

Point the CLI at the daemon:

```bash
gvl config set-url http://127.0.0.1:8080
gvl config set-token some-secret

gvl schedule wizard
# or:
gvl schedule set-wake 07:00 --duration 30 --from-color blue --from-brightness 5 --to-temp daylight --to-brightness 55
gvl schedule set-sleep 23:00 --duration 20 --end-off --split 20
gvl schedule list
gvl schedule upcoming
gvl schedule run-now wake-0700
gvl schedule preview sleep-100-days-chal

# one-shot: recurring 07:00 / 23:00 stay put
gvl schedule skip weekday-wake                 # sleep in; skip tomorrow morning
gvl schedule skip weekday-wake --count 3       # skip the next 3 wakes
gvl schedule next weekday-wake --at 09:30      # just tomorrow (or next occurrence)
gvl schedule next weekday-sleep --at 01:00 --next-day
gvl schedule next weekday-wake --at 09:00 --count 2 --duration 45 --to-temp daylight
gvl schedule next weekday-wake --clear
```

Wake ramps turn the light on and ease from a start look to an end look over N minutes.  
Sleep ramps dim toward a warm/low look and can power off at the end.

The H60A1 white LEDs bottom out at **2700K** over LAN (the app’s 2200K mixes RGB into
warm-white; `colorwc` kelvin does not). Sleep targets below that — including candle
1800K — switch to RGB orange so the evening ramp actually turns into a colour, then
`end_off` if set.

`--split 20` puts **20%** of the duration on the start look (white CCT) and **80%** on
the end look (colour). Wake is the inverse: that percent is the start colour, the rest
is white. Omit or `0` for automatic (kelvin-span, or 50/50 for colour→white).

## Networking options

1. **Same LAN** — `gvld` and the light on one network. Simplest.
2. **CLI over Tailscale** — run `gvld` on the LAN host; set `gvl config set-url https://gvld.your-tailnet.ts.net` (or HTTP on a private route). Only the daemon needs LAN reachability to the bulb.
3. **Routed LAN / subnet router** — `gvld` may run elsewhere if UDP to the device IP is routable (e.g. Tailscale subnet routes).

Device IPs and tokens stay in env / local config — never commit them.

## Debug / slow or failed commands

Govee LAN is UDP. Each command **sends twice**, waits **200ms+** for the bulb to settle, then **polls status on UDP 4002** (up to 5 retries). A happy path is ~0.3–1s; a mismatch can take several seconds. Status listen binds port 4002 exclusively, so overlapping commands used to fail with `address already in use` or a stale colour mismatch.

`gvld` now **serializes** device ops (later commands wait instead of colliding). CLI traces:

```bash
gvl -v colour blue
GVL_DEBUG=1 gvl set on colour red bright 40
```

Logs go to stderr (`http POST /v1/device … 800ms`, `exec color attempt 1/5 settle=200ms`). `gvld` always logs one line per request; `GVL_DEBUG=1` adds UDP attempt detail. Redeploy `gvld` for the serialize fix to take effect on the daemon host.

## Docker

Govee replies on UDP, so **host networking** (or equivalent LAN access) is required:

```bash
docker build -f deploy/Dockerfile -t gvl .
docker run --rm --network host \
  -e GVL_DEVICE_IP=192.0.2.10 \
  -e GVL_TOKEN=secret \
  -e GVL_TZ=UTC \
  -v gvl-data:/data \
  gvl
```

Bridge networking usually breaks status/discovery because UDP replies never reach the container.

## Personal cloud (`pc`)

If you use [personal-cloud](https://github.com/Chris-Alexander-Pop/personal-cloud):

```bash
cp deploy/.env.example ~/.config/pc/env/gvl.env   # or: pc env init
# edit secrets / device IP
pc validate
pc ship --private --wait
```

Use compose template **`with-data-volume-host`** (already set in `.personal-cloud.yaml`) so the daemon stays on the host network across ships — required for Govee LAN UDP.

Point the CLI at the private route or the VM Tailscale IP on port 8080:

```bash
gvl config set-url http://100.x.x.x:8080   # or https://gvl.<your-tailnet>
gvl config set-token <same-as-GVL_TOKEN>
gvl schedule wizard
```

## API sketch

| Method | Path | Notes |
|--------|------|--------|
| GET | `/health` | no auth |
| GET | `/v1/status` | Bearer if `GVL_TOKEN` set |
| POST | `/v1/device` | `{"cmd":"on\|off\|brightness\|color\|temp",...}` |
| POST | `/v1/mode` | mode config JSON |
| POST | `/v1/stop` | stop mode/ramp |
| GET/PUT | `/v1/schedules` | list / create (`upcoming` is computed) |
| GET/PUT/DELETE | `/v1/schedules/{id}` | |
| POST | `/v1/schedules/{id}/run` | fire now |
| POST | `/v1/schedules/{id}/preview` | play ramp as fast as the bulb confirms |
| POST | `/v1/schedules/{id}/skip` | `{count, date}` skip next occurrence(s) |
| GET/POST/DELETE | `/v1/schedules/{id}/next` | one-shot override (time / look / skip) |

## License

MIT
