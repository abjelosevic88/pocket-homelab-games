# Reverse proxy

Pocket Games speaks plain HTTP on one port and serves the UI, `/api/v1/*`, the RomM-compatible `/api/*`, the player (`/play/<id>`) and its files (`/emulatorjs/*`) from the same binary. Set `BASE_URL` to the public URL and `SECURE_COOKIES=true` behind HTTPS.

Two things matter for a proxy:

- **Big responses.** Game downloads can be many gigabytes and are sent with ranges (resumable). Don't buffer responses and don't set short read timeouts.
- **Uploads** arrive in chunks of 16 MB; allow request bodies of at least 64 MB.
- **Headers.** The player page sends `Cross-Origin-Opener-Policy` and `Cross-Origin-Embedder-Policy` (some emulators need threads). Don't strip them.

## Caddy

```
games.example.com {
	reverse_proxy pocket-games:8080 {
		flush_interval -1
	}
}
```

## Traefik (labels)

```yaml
labels:
  - traefik.http.routers.games.rule=Host(`games.example.com`)
  - traefik.http.services.games.loadbalancer.server.port=8080
```

## nginx

```
location / {
    proxy_pass http://pocket-games:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_read_timeout 1h;
    client_max_body_size 64m;
}
```

## Forward auth

With `AUTH_MODE=proxy` the app trusts `Remote-User` / `Remote-Email`. RomM apps on handhelds can't pass a forward-auth login: exclude `/api/*` (but not `/api/v1/*`) and `/assets/romm/*` if you use them — they sign in on their own (password, token or pairing code).
