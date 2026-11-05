# Architecture

The diagrams follow the [C4 model](https://c4model.com) levels (context, containers, components) and add a deployment view, the data model and three sequence diagrams. They are [Mermaid](https://mermaid.js.org), so GitHub renders them and CI checks that they parse. Every statement below was checked against the code at the time of writing; if you change behaviour, change the diagram in the same pull request.

Related: [DOC.md](DOC.md) (concept and algorithms), [OPERATIONS.md](OPERATIONS.md) (running it), [TESTING.md](TESTING.md) (what CI verifies).

## 1. System context

Who and what interacts with AVENGE.

```mermaid
flowchart LR
    operator(["Operator<br/>web browser"])
    machine["Machine with a vibration sensor<br/>ADXL345 over USB serial or RS-485,<br/>or the firmware simulator"]

    subgraph avenge["AVENGE"]
        edge["Edge engine<br/>signal features and anomaly state<br/>computed next to the sensor"]
        core["Core stack<br/>transport, storage, dashboards"]
    end

    machine -->|"raw samples<br/>binary serial packets or JSONL on stdin"| edge
    edge -->|"compact telemetry and anomaly events<br/>MQTT, authenticated, plaintext"| core
    core -->|"dashboards over HTTP"| operator
```

## 2. Containers

The deployable units and the protocols between them. Names match `edge/compose.yaml` and `core/compose.yaml`.

```mermaid
flowchart LR
    operator(["Operator<br/>web browser"])
    sensor["Vibration sensor"]

    subgraph pi["Edge device (Raspberry Pi)"]
        edgeapp["avenge-edge<br/>Go binary in a distroless,<br/>non-root container"]
    end

    subgraph host["Core host (docker compose)"]
        mosq["mosquitto<br/>MQTT broker<br/>authentication and per-user ACL<br/>network: iot"]
        tg["telegraf<br/>MQTT consumer<br/>network: iot"]
        ch[("clickhouse<br/>database iot<br/>network: iot")]
        init["clickhouse-init<br/>one-shot schema job<br/>network: iot"]
        gf["grafana<br/>dashboards<br/>networks: iot and proxy"]
        caddy["caddy<br/>reverse proxy<br/>network: proxy"]
    end

    sensor -->|"serial, 3-byte packets"| edgeapp
    edgeapp -->|"MQTT 3.1.1, QoS 1<br/>port 1883, user edge"| mosq
    mosq -->|"sm/telemetry/# and sm/anomaly/#<br/>user telegraf, read only"| tg
    tg -->|"HTTP INSERT, port 8123<br/>user telegraf, insert only"| ch
    init -->|"applies init.sql<br/>admin account"| ch
    gf -->|"SQL over HTTP, port 8123<br/>user grafana, read only"| ch
    caddy -->|"port 3000"| gf
    operator -->|"http://grafana.localhost"| caddy
```

`preflight` (credential checks) is also a one-shot container; it appears in the startup sequence in section 6.

## 3. Components of the edge engine

Inside the `edge-filter` process (`edge/`). One worker goroutine per sensor processes fixed windows of samples.

```mermaid
flowchart TB
    src["Sample source<br/>SerialReader (Linux), stdin JSONL reader<br/>or stdin PCM reader"]
    broker["Mosquitto"]

    subgraph proc["edge-filter process"]
        envf["envfile<br/>loads .env"]
        prof["profile<br/>SENSOR_PROFILE resolves window, bands,<br/>thresholds and time constants"]
        main["main<br/>wiring, signals, shutdown"]
        mgr["sensor.Manager<br/>one worker per sensor"]

        subgraph worker["sensor.SensorWorker"]
            ring["window buffer<br/>512 samples"]
            fftp["fft.Processor<br/>power spectrum"]
            band["anomaly.BandDetector<br/>z-score per frequency band"]
            rms["anomaly.Detector<br/>RMS z-score with hysteresis"]
            score["scoring.Combine<br/>max of both z-scores"]
            sdt["filter.SDT<br/>compression ratio of buffered samples"]
            out["WorkerEvent<br/>telemetry flush or anomaly transition"]
        end

        pub["mqtt.Publisher<br/>topics and JSON payloads"]
        rc["mqtt.ResilientClient<br/>queue of 512, reconnect with backoff"]
        cl["mqtt.Client<br/>MQTT 3.1.1 over TCP"]
    end

    src -->|"samples"| main
    envf -.->|"environment"| prof
    prof -.->|"SensorConfig"| main
    main -->|"Send (live) or SendBlocking (replay)"| mgr
    mgr --> ring
    ring --> fftp
    fftp --> band
    ring --> rms
    band --> score
    rms --> score
    ring -.->|"every sample"| sdt
    score --> out
    sdt -->|"every 20th window"| out
    rms -->|"state change"| out
    out -->|"events channel"| main
    main --> pub
    pub --> rc
    rc --> cl
    cl -->|"PUBLISH, QoS 1"| broker
```

Notes: `Send` drops a sample when the worker's input buffer is full, which is right for a live sensor. `SendBlocking` applies backpressure instead and is used for replayed input (file or pipe), so replays are reproducible.

## 4. Deployment and exposure

Which ports are reachable from where. MQTT is the only service meant to be reachable from other machines.

```mermaid
flowchart LR
    subgraph lan["Trusted LAN"]
        subgraph pi["Raspberry Pi"]
            dev["serial device<br/>one path passed in, SERIAL_PORT"]
            edge["avenge-edge container<br/>user 65532, added to the dialout GID"]
            dev --> edge
        end

        subgraph srv["Core host"]
            p1883["port 1883<br/>bound to MQTT_BIND"]
            p80["port 80<br/>bound to 127.0.0.1"]
            p8123["ports 8123 and 9000<br/>bound to 127.0.0.1"]
            nogf["Grafana publishes no port"]
        end
    end

    browser(["Browser on the core host"])
    admin(["Admin on the core host<br/>clickhouse-client, curl"])

    edge -->|"MQTT, plaintext, user edge"| p1883
    browser --> p80
    admin --> p8123
```

Set `MQTT_BIND` to the host's LAN address so the Pi can connect; the default `127.0.0.1` keeps the broker local. MQTT is not encrypted, so keep it on a trusted network.

## 5. Data model

How a message becomes rows. Retention is applied by ClickHouse TTLs (see [OPERATIONS.md](OPERATIONS.md#data-retention)).

```mermaid
flowchart LR
    t1["MQTT topic<br/>sm/telemetry/sensor_id"]
    t2["MQTT topic<br/>sm/anomaly/sensor_id"]
    tg["Telegraf<br/>json_v2 parser, 5 s batches,<br/>JSON batch over HTTP"]

    raw[("iot.telemetry_raw<br/>one JSON batch per row<br/>kept 7 days")]
    mvt["mv_telemetry_parse"]
    mva["mv_anomaly_parse"]
    tel[("iot.telemetry<br/>kept 30 days")]
    ev[("iot.anomaly_events<br/>kept 90 days")]
    mv1["mv_telemetry_1m"]
    t1m[("iot.telemetry_1m<br/>per-minute aggregate<br/>kept indefinitely")]
    graf["Grafana dashboard<br/>Vibration Monitor"]

    t1 --> tg
    t2 --> tg
    tg -->|"INSERT FORMAT JSONAsString"| raw
    raw --> mvt
    raw --> mva
    mvt --> tel
    mva --> ev
    tel --> mv1
    mv1 --> t1m
    tel --> graf
    ev --> graf
    t1m --> graf
```

## 6. Sequences

### Startup: one command

`docker compose up -d` orders everything with `depends_on` conditions. A bad password stops the whole start at `preflight`.

```mermaid
sequenceDiagram
    actor U as You
    participant C as docker compose
    participant P as preflight
    participant M as mosquitto
    participant K as clickhouse
    participant I as clickhouse-init
    participant T as telegraf
    participant G as grafana
    participant W as caddy

    U->>C: docker compose up -d
    C->>P: start
    P->>P: check every password is set, not changeme, letters and digits only
    alt a password is bad
        P-->>C: exit 1 with an ERROR line
        C-->>U: nothing else starts
    else all passwords are good
        P-->>C: exit 0
    end
    par broker
        C->>M: start, build the password file from .env
        M-->>C: healthy
    and database
        C->>K: start, load the service accounts
        K-->>C: healthy
    end
    C->>I: start once clickhouse is healthy
    I->>K: create database and apply init.sql, safe to repeat
    I-->>C: exit 0
    par consumer
        C->>T: start once mosquitto is healthy and the schema exists
    and dashboards
        C->>G: start once the schema exists
        G-->>C: healthy
        C->>W: start once grafana is healthy
    end
    C-->>U: stack is up
```

### Data path: from sample to dashboard

```mermaid
sequenceDiagram
    participant S as Sensor
    participant E as Edge worker
    participant R as Resilient client
    participant B as Mosquitto
    participant T as Telegraf
    participant K as ClickHouse
    participant G as Grafana

    loop every sample
        S->>E: sample
        E->>E: add to the window and to the SDT buffer
    end
    Note over E: 512 samples make one window
    E->>E: remove DC, FFT, band z-scores, RMS z-score, combine
    opt the RMS detector changes state
        E->>R: sm/anomaly/id with event_type onset or clear
    end
    opt every 20th window
        E->>E: compute the SDT compression ratio of the buffered samples
        E->>R: sm/telemetry/id with severity, final_score, dominant_signal
    end
    R->>B: PUBLISH, QoS 1, user edge
    B-->>R: PUBACK
    B->>T: deliver to the subscriber, user telegraf
    T->>T: batch for up to 5 seconds
    T->>K: HTTP INSERT into telemetry_raw, user telegraf
    K->>K: materialized views fill telemetry, anomaly_events, telemetry_1m
    G->>K: SQL queries, user grafana, read only
```

### Broker outage and recovery

What the edge does when the broker disappears. The queue lives in memory only.

```mermaid
sequenceDiagram
    participant E as Edge worker
    participant R as Resilient client
    participant B as Mosquitto

    E->>R: publish, queued without blocking
    R->>B: PUBLISH, QoS 1
    B-->>R: PUBACK
    Note over B: the broker becomes unreachable
    R->>R: connection lost, wait 1 s, then 2 s, 4 s and so on up to 60 s
    loop while the broker is down
        E->>R: publish
        alt the queue holds fewer than 512 messages
            R->>R: keep the message in memory
        else the queue is full
            R->>R: drop the new message and count it
        end
        R->>B: reconnect attempt, fails
    end
    Note over B: the broker returns
    R->>B: connect, backoff resets to 1 s
    R->>B: send the queued messages in order, each waits up to 3 s for PUBACK
    Note over R: a publish that fails midway is queued again at the back, or dropped if the queue is full
    Note over E,R: queued messages are lost if the process stops
```

## Notes and limits

- **Anomaly events come from the RMS detector only.** A message on `sm/anomaly/...` is published when the RMS detector changes state, and its `event_type` is `onset` only while that detector reports an anomaly; every other transition, including a first warning, is labelled `clear`. The combined severity (RMS and frequency bands) travels in the telemetry messages, so a fault visible only in a frequency band shows up in telemetry but may produce no anomaly event.
- **Outage buffer.** With the default cadence the queue of 512 messages holds roughly `512 × 20 × 512 / sample_rate` seconds of telemetry: about 87 minutes at 1 kHz and 55 minutes at 1.6 kHz. This is derived from the code, not measured.
- **SDT is a measurement, not a payload.** The engine compresses the buffered samples to report the compression ratio (`sdt_ratio`); the compressed points are not transmitted. One telemetry message summarizes a whole flush interval.
- **Allocations.** After initialization the per-window path (ring buffer, FFT, both detectors, scoring) does not allocate; tests enforce this. A window that emits an event allocates the event and a copy of the band states (a few small objects), because they are handed to another goroutine. This is a measured property of the code on amd64 with Go 1.22; the Raspberry Pi has not been measured.
- **Timestamps from the serial port are synthesized.** The firmware sends no timestamps; the reader derives them from a counter and `SENSOR_SAMPLE_RATE`, so that value must match the firmware's real rate.
- **No transport encryption.** MQTT is plaintext, authenticated and ACL-restricted; run it on a trusted network.
