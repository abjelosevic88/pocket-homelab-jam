# Reverse proxy

- **HTTPS**: needed for the microphone (browsers refuse it on plain http, except localhost).
- **Uploads**: each take and mix is one request (a long WAV can be a few hundred MB). If your proxy limits request bodies, allow at least 1 GB.
- **Caddy**: see `deploy/reverse-proxy/Caddyfile`.
- Set `BASE_URL` to the public address and `SECURE_COOKIES=true`.
