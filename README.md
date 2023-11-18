# Tutlor Mysql Agent

To be run in micro vm

Example requests
```bash
curl -X POST -H "Content-Type: application/json" -d '{"id": "ae89e354-b939-4b1c-ade1-d784119dc610","database": "mysql", "code": "create database obrs_ci;", "raw": true}' http://0.0.0.0:8080/run
```