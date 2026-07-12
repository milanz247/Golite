# Golite Documentation

- [Architecture Overview](architecture.md) — folder structure, how pieces map to Laravel, key design decisions
- [Bootstrapping Process](bootstrapping.md) — exactly what happens, in order, from `NewApplication()` to `ListenAndServe`
- [Request Lifecycle](request-lifecycle.md) — how a single HTTP request flows through the kernel, middleware, and controller
- [Service Container](service-container.md) — `Bind`/`Make`, thread safety, avoiding import cycles
- [Service Providers](service-providers.md) — the `Register`/`Boot` contract, writing your own provider
- [Routing](routing.md) — the regex route engine: verbs, parameters, `where*` constraints, named routes & URL generation, groups, redirects, fallback, 405 handling
- [Middleware](middleware.md) — global/route/grouped middleware registries, priority sorting, parameterized middleware, `WithoutMiddleware`, terminable middleware, container-resolved DI
- [CSRF Protection](security-csrf.md) — the session mechanism, `Context.CsrfToken()`, `VerifyCsrfToken`, wildcard exclusions, the `XSRF-TOKEN` cookie
- [HTTP Request Handling](http-requests.md) — request inspection helpers, the unified input payload, encrypted cookies, flash/old input, file uploads, `TrimStrings`/`ConvertEmptyStringsToNull`/`TrustProxies`/`TrustHosts`
- [Configuration](configuration.md) — `.env`, `config.LoadConfig()`, adding new config values
- [Developer Guide](developer-guide.md) — practical how-tos: running the app, adding routes/services/providers/middleware, known limitations
