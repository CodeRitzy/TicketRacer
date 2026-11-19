# Ticketracer

A ticketing system that prevents overselling when thousands of people compete for limited seats.

Pick a show near you, wait in line, hold seats for 10 minutes, and pay. PostgreSQL decides every hold, so a seat is never sold twice. Expired holds go back on sale, and a webhook delivered five times still makes one order. A dashboard with a traffic simulator tries to break all of it.

**Stack:** React + TypeScript · Go · PostgreSQL · Redis · Stripe · Docker Compose

## Run it

Needs only [Docker Desktop](https://www.docker.com/products/docker-desktop/). No setup or `.env` file required.

```bash
docker compose up --build   # then open http://localhost:8080
```

- **Book:** pick a show, join the queue, choose seats, hold, pay. Payments use a built-in demo checkout. Each browser tab is its own fan.
- **Dashboard:** click *Watch it under load* on a show, enter the admin token `localpassword1234567890`, then release the fans.

`localpassword1234567890` is a public default for local use only. The API refuses to start with it unless the site runs on localhost. To use your own admin token, set `ADMIN_TOKEN` in `.env` to any value of 24+ characters.

Your state is guessed from your device's time zone (no location permission needed). If it can't be told, the page opens on California, and any other state is one click away.

## Optional: real Stripe test mode

Needs a free [Stripe](https://dashboard.stripe.com/register) account and the [Stripe CLI](https://docs.stripe.com/stripe-cli).

1. Install the Stripe CLI and log in:
   ```bash
   brew install stripe/stripe-cli/stripe                                         # macOS
   scoop bucket add stripe https://github.com/stripe/scoop-stripe-cli.git; scoop install stripe   # Windows
   stripe login
   ```
   Other systems: see the [install guide](https://docs.stripe.com/stripe-cli).
2. Create your config and switch to Stripe:
   ```bash
   cp .env.example .env
   ```
   In `.env`, set `PAYMENT_MODE=stripe` and `STRIPE_SECRET_KEY=sk_test_…` (Stripe Dashboard → Developers → API keys).
3. Forward Stripe's webhooks to the app, and copy the `whsec_…` secret it prints into `STRIPE_WEBHOOK_SECRET` in `.env`. Leave it running.
   ```bash
   stripe listen --events checkout.session.completed,checkout.session.async_payment_succeeded,checkout.session.expired --forward-to localhost:8080/api/webhooks/stripe
   ```
   In Windows PowerShell, put the event list in quotes: `--events "checkout.session.completed,checkout.session.async_payment_succeeded,checkout.session.expired"`.
4. Restart the API with `docker compose up -d --build api`, then pay with card `4242 4242 4242 4242`, any future date and any CVC.

## How overselling is prevented

- **Seats:** one transaction claims all requested seats or none (`FOR UPDATE SKIP LOCKED`).
- **Holds:** a sweeper frees expired holds every 2 seconds.
- **Payments:** a unique webhook-event id plus unique order constraints make duplicates harmless. The amount is checked against the database, and late or wrong payments are refunded.
- **Waiting room:** a Redis queue admits fans at a fixed rate.
- **Admin:** the dashboard and simulator endpoints need a bearer token, compared in constant time.

## Tests and load test

Needs Go and Node only if you run these outside Docker.

```bash
cd frontend && npm ci && npm test       # frontend
cd backend && go test ./internal/config ./internal/catalog   # backend unit tests
cd backend && go run ./cmd/loadtest -url http://localhost:8080 -state CA -users 2000 -stampede
```

The full backend suite runs against real Postgres and Redis; CI (`.github/workflows/ci.yml`) runs it, plus a 1,000-fan stampede, on every push.

## Notes

- Acts are fictional. Venues and capacities are real; prices are typical, not actual.
- This is a local demo. Before deploying: set long random values for every password in `.env`, use HTTPS, and unset `DEMO_WEBHOOK_SECRET`.
- `.env` is gitignored, so your keys stay on your machine.

## Layout

```
backend/    cmd/{api,loadtest} · internal/{catalog,config,store,queue,pay,api,sim}
frontend/   src/{api.ts, lib, components, pages} · nginx.conf
docker-compose.yml · .github/workflows/ci.yml
```
