# Go-url-shortner
Backend URL shortener service built with Go, PostgreSQL, and Gorilla Mux. Provides API endpoints for creating and retrieving shortened URLs, with graceful shutdown support.

## Current status
- [x] Basic URL shortening functionality and analytics
- [x] Graceful shutdown and structured logging
- [ ] Unit tests for handlers and database interactions
- [ ] Dockerization and deployment scripts
- [ ] Rate limiting and analytics features
- [ ] Frontend interface for URL management
- [ ] CI/CD pipeline setup
- [ ] Documentation and API specs
- [ ] Performance optimizations and security hardening
- [ ] Monitoring and alerting integration
- [ ] User authentication and management
- [ ] Custom URL aliases and expiration features
- [ ] Internationalization and localization support
- [ ] Caching layer for improved performance
- [ ] Load testing and scalability improvements

## Caching benchmarks
Command:
```bash
hey -n 1000 -c 50 -disable-redirects http://localhost:3000/MRUM5V
```

latest local benchmarking Results  (May 27, 2026):
```
Summary:
  Total:        0.9221 secs
  Slowest:      0.0804 secs
  Fastest:      0.0007 secs
  Average:      0.0091 secs
  Requests/sec: 5422.3066

  Total data:   330000 bytes
  Size/request: 66 bytes

Response time histogram:
  0.001 [1]     |
  0.009 [3426]  |■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■
  0.017 [1178]  |■■■■■■■■■■■■■■
  0.025 [181]   |■■
  0.033 [88]    |■
  0.041 [28]    |
  0.048 [27]    |
  0.056 [22]    |
  0.064 [22]    |
  0.072 [19]    |
  0.080 [8]     |


Latency distribution:
  10%% in 0.0043 secs
  25%% in 0.0054 secs
  50%% in 0.0070 secs
  75%% in 0.0094 secs
  90%% in 0.0145 secs
  95%% in 0.0232 secs
  99%% in 0.0557 secs

Details (average, fastest, slowest):
  DNS+dialup:   0.0001 secs, 0.0000 secs, 0.0122 secs
  DNS-lookup:   0.0001 secs, 0.0000 secs, 0.0105 secs
  req write:    0.0001 secs, 0.0000 secs, 0.0050 secs
  resp wait:    0.0085 secs, 0.0006 secs, 0.0732 secs
  resp read:    0.0004 secs, 0.0000 secs, 0.0067 secs

Status code distribution:
  [302] 5000 responses
```
Stack: Go + PostgreSQL + Redis