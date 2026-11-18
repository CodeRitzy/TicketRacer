# Ticketracer

A ticketing system that prevents overselling when thousands of people compete for limited seats.

Pick a show near you, wait in line, hold seats for 10 minutes, and pay with Stripe (test mode). PostgreSQL decides every hold, so a seat is never sold twice. Expired holds go back on sale, and a webhook delivered five times still makes one order. A dashboard with a traffic simulator tries to break all of it.

**Stack:** React + TypeScript · Go · PostgreSQL · Redis · Docker Compose

## Run it

Needs Docker Desktop.

```bash
cp .env.example .env        # replace every "change-me" with a random value (24+ characters)
docker compose up --build   # open http://localhost:8080
```

- **Book:** pick a show, join the queue, choose seats, hold, pay. Each browser tab is its own fan.
- **Dashboard:** click *Watch it under load* on a show, enter `ADMIN_TOKEN` from `.env`, then release the fans.

## Payments

`PAYMENT_MODE=demo` (the default) needs no Stripe account. For real Stripe test mode:

1. Set `STRIPE_SECRET_KEY=sk_test_…` and `PAYMENT_MODE=stripe` in `.env`.
2. Forward webhooks and copy the printed secret into `STRIPE_WEBHOOK_SECRET`:
   ```bash
   stripe listen --events checkout.session.completed,checkout.session.async_payment_succeeded,checkout.session.expired --forward-to localhost:8080/api/webhooks/stripe
   ```
3. `docker compose up -d --build api`, then pay with card `4242 4242 4242 4242`.

## How overselling is prevented

- **Seats:** one transaction claims all requested seats or none (`FOR UPDATE SKIP LOCKED`).
- **Holds:** a sweeper frees expired holds every 2 seconds.
- **Payments:** a unique webhook-event id plus unique order constraints make duplicates harmless. The amount is checked against the database, and late or wrong payments are refunded.
- **Waiting room:** a Redis queue admits fans at a fixed rate.

## Tests and load test

```bash
cd frontend && npm ci && npm test                       # frontend
cd backend && go test -p 1 ./...                        # needs Postgres + Redis, see CI for setup
cd backend && go run ./cmd/loadtest -url http://localhost:8080 -state CA -users 2000 -stampede
```

CI (`.github/workflows/ci.yml`) runs all of this, plus a 1,000-fan stampede, on every push.

## Notes

- Acts are fictional. Venues and capacities are real; prices are typical, not actual.
- This is a local demo. Before deploying: use HTTPS and unset `DEMO_WEBHOOK_SECRET`.
- Secrets live only in `.env`, which is gitignored.

## Layout

```
backend/    cmd/{api,loadtest} · internal/{catalog,config,store,queue,pay,api,sim}
frontend/   src/{api.ts, lib, components, pages} · nginx.conf
docker-compose.yml · .github/workflows/ci.yml
```
