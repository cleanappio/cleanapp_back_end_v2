# CleanApp Prod Xray (As Deployed)

Snapshot folder: `/Users/anon16/Downloads/cleanapp_back_end_v2/xray/prod/2026-02-07`

## Host

```
Sat Feb  7 00:18:49 UTC 2026
cleanapp-prod
Linux cleanapp-prod 6.1.0-41-cloud-amd64 #1 SMP PREEMPT_DYNAMIC Debian 6.1.158-1 (2025-11-09) x86_64 GNU/Linux
Docker version 28.3.1, build 38b7060
Docker Compose version v2.38.1
nginx version: nginx/1.22.1
```

## Running Containers (Snapshot)

Columns: name, compose_service, health, published_ports, image, repo_digest

| name | compose | health | ports | image | repo digest |
|---|---|---|---|---|---|
| `093e3a8bfd19_cleanapp_report_listener_v4` | `cleanapp_report_listener_v4` | `none` | `8080/tcp -> 0.0.0.0:9097;8080/tcp -> [::]:9097` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-listener-v4-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-listener-v4-image@sha256:1a4ee597071c6384a26a09b9f84e7dc02376660d32e3deb49f6a2442bc3f536c` |
| `5c8f50873b4c_cleanapp_email_service` | `cleanapp_email_service` | `none` | `8080/tcp -> 0.0.0.0:9089;8080/tcp -> [::]:9089` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-email-service-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-email-service-image@sha256:df393ac8738f1c54fad552d8e5f176fe8219c68af66df25d768a842743262647` |
| `84ea7885beaa_cleanapp_areas_service` | `cleanapp_areas_service` | `none` | `8080/tcp -> 0.0.0.0:9086;8080/tcp -> [::]:9086` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-areas-service-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-areas-service-image@sha256:e2885fe492f0a275f4f9ffd8baa2d4dc4770f919be7ff062ad768b88ac4b9c17` |
| `cleanapp_bluesky_analyzer` | `(no-compose)` | `none` | `` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-bluesky-analyzer-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-bluesky-analyzer-image@sha256:3aaf75555c3903d6c2ba1f871b496be2fbc491b0e64cd341205597a1f3ed8dcf` |
| `cleanapp_bluesky_indexer` | `cleanapp_bluesky_indexer` | `none` | `` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-bluesky-indexer-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-bluesky-indexer-image@sha256:842494ac7ec7ce125b45a8e4e3d7e0570dd30b3564e73d4255f8659e0e0e509a` |
| `cleanapp_bluesky_now` | `(no-compose)` | `none` | `` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/bluesky-now:v3` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/bluesky-now@sha256:d93c7da4ff8a5a28c9c13e91066b7f384a92c3f189defc9aa7635e6ebbda3c1c` |
| `cleanapp_bluesky_submitter` | `cleanapp_bluesky_submitter` | `none` | `` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-bluesky-submitter-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-bluesky-submitter-image@sha256:e8db5c5f40c7b18af5baa0e7a20c4e0fef1faef22d41ab4428fd0abcd775155c` |
| `cleanapp_customer_service` | `cleanapp_customer_service` | `none` | `8080/tcp -> 0.0.0.0:9080;8080/tcp -> [::]:9080` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-customer-service-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-customer-service-image@sha256:c44cb9d2cc4860d2c789f14f612a8f046955f6a22be43b054bc2265fbf347e9b` |
| `cleanapp_db` | `cleanapp_db` | `healthy` | `3306/tcp -> 0.0.0.0:3306;3306/tcp -> [::]:3306` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-db-image:live` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-db-image@sha256:03d76343bb098771f86337e8c28a76b6cc3412f7306b6806880a8ef6eef598c0` |
| `cleanapp_devconnect_2025_areas` | `cleanapp_devconnect_2025_areas` | `none` | `8080/tcp -> 0.0.0.0:9094;8080/tcp -> [::]:9094` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-custom-area-dashboard-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-custom-area-dashboard-image@sha256:86d2eec3edb9e9f9a76d5848c0bd799d6313af5d4bb75bf5c33ca87d03f8a5b1` |
| `cleanapp_edge_city_areas` | `cleanapp_edge_city_areas` | `none` | `8080/tcp -> 0.0.0.0:9095;8080/tcp -> [::]:9095` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-custom-area-dashboard-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-custom-area-dashboard-image@sha256:86d2eec3edb9e9f9a76d5848c0bd799d6313af5d4bb75bf5c33ca87d03f8a5b1` |
| `cleanapp_email_fetcher` | `cleanapp_email_fetcher` | `none` | `` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-email-fetcher-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-email-fetcher-image@sha256:b3d95906977e30756b1a75856193d1694c0fe42ec26318372482fda5a4f68e4f` |
| `cleanapp_frontend` | `(no-compose)` | `none` | `3000/tcp -> 0.0.0.0:3001;3000/tcp -> [::]:3001` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-frontend-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-frontend-image@sha256:d8aeaf8184f4d4010e759b8802907d7069cd0e6215e7c1443a6de599e9f30a4d` |
| `cleanapp_frontend_embedded` | `(no-compose)` | `none` | `3000/tcp -> 0.0.0.0:3002;3000/tcp -> [::]:3002` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-frontend-image-embedded:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-frontend-image-embedded@sha256:cfcf7b4c136660a640516856bdda8985e08392d727d53807e100e5c1adafd761` |
| `cleanapp_montenegro_areas` | `cleanapp_montenegro_areas` | `none` | `8080/tcp -> 0.0.0.0:9083;8080/tcp -> [::]:9083` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-custom-area-dashboard-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-custom-area-dashboard-image@sha256:86d2eec3edb9e9f9a76d5848c0bd799d6313af5d4bb75bf5c33ca87d03f8a5b1` |
| `cleanapp_new_york_areas` | `cleanapp_new_york_areas` | `none` | `8080/tcp -> 0.0.0.0:9088;8080/tcp -> [::]:9088` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-custom-area-dashboard-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-custom-area-dashboard-image@sha256:86d2eec3edb9e9f9a76d5848c0bd799d6313af5d4bb75bf5c33ca87d03f8a5b1` |
| `cleanapp_news_analyzer_twitter` | `cleanapp_news_analyzer_twitter` | `none` | `` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-news-analyzer-twitter-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-news-analyzer-twitter-image@sha256:cb15e9afbeef94450b7bd15d2b755789db8e3ff41a33f25dafcf9095eedcf9f3` |
| `cleanapp_news_indexer_twitter` | `cleanapp_news_indexer_twitter` | `none` | `` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-news-indexer-twitter-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-news-indexer-twitter-image@sha256:15b4f2d82f31e38153e3a5747b507a2438a73296182bbf93fcf59da7937d04bb` |
| `cleanapp_pipelines` | `cleanapp_pipelines` | `none` | `8090/tcp -> 0.0.0.0:8090;8090/tcp -> [::]:8090` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-pipelines-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-pipelines-image@sha256:73c927f20452cdd9b9605b5d5fbe16032a942c0c06add2a43b80c94bff9c5428` |
| `cleanapp_rabbitmq` | `cleanapp_rabbitmq` | `healthy` | `5672/tcp -> 0.0.0.0:5672;5672/tcp -> [::]:5672;15672/tcp -> 0.0.0.0:15672;15672/tcp -> [::]:15672` | `rabbitmq:latest` | `rabbitmq@sha256:3f668940cdeaff4870d1f2310ec6437360331e11ed24b0c3ce1af9607ccccb93` |
| `cleanapp_red_bull_dashboard` | `cleanapp_red_bull_dashboard` | `healthy` | `8080/tcp -> 0.0.0.0:9085;8080/tcp -> [::]:9085` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-brand-dashboard-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-brand-dashboard-image@sha256:9022e6b2726ec55af0d0f39dd3507ed8c6c6b49c6812f1450a0e2645d063c009` |
| `cleanapp_replier_twitter` | `cleanapp_replier_twitter` | `none` | `` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-news-replier-twitter-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-news-replier-twitter-image@sha256:44def08e66edbe5f7c8af38d4414149063686b54e8e7bea63713c1b6c9709cc8` |
| `cleanapp_report_analyze_pipeline` | `(no-compose)` | `none` | `8080/tcp -> 0.0.0.0:9082;8080/tcp -> [::]:9082` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-analyze-pipeline-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-analyze-pipeline-image@sha256:41f89e2fedba4d922c4f7da0fbad6c2222868ff34659f78ff6d4b48cec7c5f38` |
| `cleanapp_report_listener` | `cleanapp_report_listener` | `healthy` | `8080/tcp -> 0.0.0.0:9081;8080/tcp -> [::]:9081` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-listener-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-listener-image@sha256:68e22f791cbff80e16902c95c6957224910e197b7a77874e7ab3d5b3ffdda04e` |
| `cleanapp_report_processor` | `cleanapp_report_processor` | `none` | `8080/tcp -> 0.0.0.0:9087;8080/tcp -> [::]:9087` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-processor-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-processor-image@sha256:847e2e067f260d17fb9fe858689bd95ddd2f809d71005975cb60e9952a0af84e` |
| `cleanapp_report_renderer_service` | `cleanapp_report_renderer_service` | `healthy` | `8080/tcp -> 0.0.0.0:9093;8080/tcp -> [::]:9093` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-fast-renderer-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-fast-renderer-image@sha256:693ceeb0ac73d8db7325c8651fc979411355208164763f08708ecbd21cf92098` |
| `cleanapp_report_tags_service` | `cleanapp_report_tags_service` | `healthy` | `8080/tcp -> 0.0.0.0:9098;8080/tcp -> [::]:9098` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-tags-service-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-tags-service-image@sha256:5b95b06c1c8afad5e0ada0612420988a15dfbc52d724a474249df8433c9a8315` |
| `cleanapp_service` | `(no-compose)` | `none` | `8080/tcp -> 0.0.0.0:8079;8080/tcp -> [::]:8079` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-service-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-service-image@sha256:1f720f299455878068e9a8047df209bdac12ed9e84bb153ef0375e1ce8dc1f28` |
| `cleanapp_voice_assistant_service` | `cleanapp_voice_assistant_service` | `none` | `8080/tcp -> 0.0.0.0:9092;8080/tcp -> [::]:9092` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-voice-assistant-service-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-voice-assistant-service-image@sha256:48edf7c694fbc76fbe61d65b6b4137866847ac636a0d47dd16e88534bc8fd65a` |
| `cleanapp_web` | `cleanapp_web` | `none` | `3000/tcp -> 0.0.0.0:3000;3000/tcp -> [::]:3000` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-web-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-web-image@sha256:7af5382d44379b796e7146890beb6e25f1dff5be5f4f238dee5b4f4516d72356` |
| `fd599f74b05f_cleanapp_auth_service` | `cleanapp_auth_service` | `none` | `8080/tcp -> 0.0.0.0:9084;8080/tcp -> [::]:9084` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-auth-service-image:prod` | `us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-auth-service-image@sha256:4a5f82884e6f936a36795226764ab10cc68dcb16d3bcf4f0f791e8ea37758df9` |

### Compose Drift

- Compose project containers: 25
- Non-compose (manually started) containers: 6

Non-compose container names:

- `cleanapp_bluesky_analyzer`
- `cleanapp_bluesky_now`
- `cleanapp_frontend`
- `cleanapp_frontend_embedded`
- `cleanapp_report_analyze_pipeline`
- `cleanapp_service`

## Public Routing (nginx)

| hostname | path | upstream | container | source |
|---|---|---|---|---|
| `api.cleanapp.io` | `/` | `127.0.0.1:8079` | `cleanapp_service` | `apicleanapp.conf` |
| `api.cleanapp.io` | `/` | `127.0.0.1:9080` | `cleanapp_customer_service` | `apicleanapp.conf` |
| `apiedgecity.cleanapp.io` | `/` | `127.0.0.1:9095` | `cleanapp_edge_city_areas` | `edgecity.conf` |
| `apimontenegro.cleanapp.io` | `/` | `127.0.0.1:9083` | `cleanapp_montenegro_areas` | `montenegrocleanapp.conf` |
| `apinewyork.cleanapp.io` | `/` | `127.0.0.1:9088` | `cleanapp_new_york_areas` | `newyorkcleanapp.conf` |
| `apiredbull.cleanapp.io` | `/` | `127.0.0.1:9085` | `cleanapp_red_bull_dashboard` | `redbullcleanapp.conf` |
| `areas.cleanapp.io` | `/` | `127.0.0.1:9086` | `84ea7885beaa_cleanapp_areas_service` | `areascleanapp.conf` |
| `auth.cleanapp.io` | `/` | `127.0.0.1:9084` | `fd599f74b05f_cleanapp_auth_service` | `authcleanapp.conf` |
| `cleanapp.io` | `/` | `127.0.0.1:3001` | `cleanapp_frontend` | `cleanapp.conf` |
| `cleanapp.io` | `/api/` | `127.0.0.1:9080` | `cleanapp_customer_service` | `cleanapp.conf` |
| `cleanapp.io` | `/api/geocode` | `127.0.0.1:3001` | `cleanapp_frontend` | `cleanapp.conf` |
| `cleanapp.io` | `/api/reports-count` | `127.0.0.1:3001` | `cleanapp_frontend` | `cleanapp.conf` |
| `cleanapp.io` | `/api/reports/` | `127.0.0.1:3001` | `cleanapp_frontend` | `cleanapp.conf` |
| `cleanapp.io` | `/api/v3/` | `127.0.0.1:9081` | `cleanapp_report_listener` | `cleanapp.conf` |
| `cleanapp.io` | `/api/v4/` | `127.0.0.1:9097` | `093e3a8bfd19_cleanapp_report_listener_v4` | `cleanapp.conf` |
| `devconnect2025.cleanapp.io` | `/` | `127.0.0.1:9094` | `cleanapp_devconnect_2025_areas` | `devconnect2025.conf` |
| `email.cleanapp.io` | `/` | `127.0.0.1:9089` | `5c8f50873b4c_cleanapp_email_service` | `emailcleanapp.conf` |
| `embed.cleanapp.io` | `/` | `127.0.0.1:3002` | `cleanapp_frontend_embedded` | `embeddedcleanapp.conf` |
| `live.cleanapp.io` | `/` | `127.0.0.1:9081` | `cleanapp_report_listener` | `livecleanapp.conf` |
| `live.cleanapp.io` | `/api/v3/` | `127.0.0.1:9081` | `cleanapp_report_listener` | `livecleanapp.conf` |
| `live.cleanapp.io` | `/api/v4/` | `127.0.0.1:9097` | `093e3a8bfd19_cleanapp_report_listener_v4` | `livecleanapp.conf` |
| `processing.cleanapp.io` | `/` | `127.0.0.1:9087` | `cleanapp_report_processor` | `processcleanapp.conf` |
| `renderer.cleanapp.io` | `/` | `127.0.0.1:9093` | `cleanapp_report_renderer_service` | `fastrenderercleanapp.conf` |
| `tags.cleanapp.io` | `/` | `127.0.0.1:9098` | `cleanapp_report_tags_service` | `tagcleanapp.conf` |
| `voice.cleanapp.io` | `/` | `127.0.0.1:9092` | `cleanapp_voice_assistant_service` | `voicecleanapp.conf` |
| `www.api.cleanapp.io` | `/` | `127.0.0.1:9080` | `cleanapp_customer_service` | `apicleanapp.conf` |
| `www.apiedgecity.cleanapp.io` | `/` | `127.0.0.1:9095` | `cleanapp_edge_city_areas` | `edgecity.conf` |
| `www.apimontenegro.cleanapp.io` | `/` | `127.0.0.1:9083` | `cleanapp_montenegro_areas` | `montenegrocleanapp.conf` |
| `www.apinewyork.cleanapp.io` | `/` | `127.0.0.1:9088` | `cleanapp_new_york_areas` | `newyorkcleanapp.conf` |
| `www.apiredbull.cleanapp.io` | `/` | `127.0.0.1:9085` | `cleanapp_red_bull_dashboard` | `redbullcleanapp.conf` |
| `www.areas.cleanapp.io` | `/` | `127.0.0.1:9086` | `84ea7885beaa_cleanapp_areas_service` | `areascleanapp.conf` |
| `www.auth.cleanapp.io` | `/` | `127.0.0.1:9084` | `fd599f74b05f_cleanapp_auth_service` | `authcleanapp.conf` |
| `www.cleanapp.io` | `/` | `127.0.0.1:3001` | `cleanapp_frontend` | `cleanapp.conf` |
| `www.cleanapp.io` | `/api/` | `127.0.0.1:9080` | `cleanapp_customer_service` | `cleanapp.conf` |
| `www.cleanapp.io` | `/api/geocode` | `127.0.0.1:3001` | `cleanapp_frontend` | `cleanapp.conf` |
| `www.cleanapp.io` | `/api/reports-count` | `127.0.0.1:3001` | `cleanapp_frontend` | `cleanapp.conf` |
| `www.cleanapp.io` | `/api/reports/` | `127.0.0.1:3001` | `cleanapp_frontend` | `cleanapp.conf` |
| `www.cleanapp.io` | `/api/v3/` | `127.0.0.1:9081` | `cleanapp_report_listener` | `cleanapp.conf` |
| `www.cleanapp.io` | `/api/v4/` | `127.0.0.1:9097` | `093e3a8bfd19_cleanapp_report_listener_v4` | `cleanapp.conf` |
| `www.devconnect2025.cleanapp.io` | `/` | `127.0.0.1:9094` | `cleanapp_devconnect_2025_areas` | `devconnect2025.conf` |
| `www.email.cleanapp.io` | `/` | `127.0.0.1:9089` | `5c8f50873b4c_cleanapp_email_service` | `emailcleanapp.conf` |
| `www.embed.cleanapp.io` | `/` | `127.0.0.1:3002` | `cleanapp_frontend_embedded` | `embeddedcleanapp.conf` |
| `www.live.cleanapp.io` | `/` | `127.0.0.1:9081` | `cleanapp_report_listener` | `livecleanapp.conf` |
| `www.live.cleanapp.io` | `/api/v3/` | `127.0.0.1:9081` | `cleanapp_report_listener` | `livecleanapp.conf` |
| `www.live.cleanapp.io` | `/api/v4/` | `127.0.0.1:9097` | `093e3a8bfd19_cleanapp_report_listener_v4` | `livecleanapp.conf` |
| `www.processing.cleanapp.io` | `/` | `127.0.0.1:9087` | `cleanapp_report_processor` | `processcleanapp.conf` |
| `www.renderer.cleanapp.io` | `/` | `127.0.0.1:9093` | `cleanapp_report_renderer_service` | `fastrenderercleanapp.conf` |
| `www.tags.cleanapp.io` | `/` | `127.0.0.1:9098` | `cleanapp_report_tags_service` | `tagcleanapp.conf` |
| `www.voice.cleanapp.io` | `/` | `127.0.0.1:9092` | `cleanapp_voice_assistant_service` | `voicecleanapp.conf` |

Notes:

- Upstream ports are host ports on the VM (nginx proxies to `127.0.0.1:<port>`).
- Some routes proxy to frontends (`3001`, `3002`) and multiple API backends.

## RabbitMQ (Topology Snapshot)

### Exchanges

```
Listing exchanges for vhost / ...
name	type	durable	auto_delete	internal
amq.rabbitmq.trace	topic	true	false	true
amq.headers	headers	true	false	false
amq.fanout	fanout	true	false	false
amq.topic	topic	true	false	false
amq.direct	direct	true	false	false
amq.match	headers	true	false	false
cleanapp	direct	true	false	false
	direct	true	false	false
cleanapp-exchange	direct	true	false	false
```

### Queues

```
Timeout: 60.0 seconds ...
Listing queues for vhost / ...
name	durable	auto_delete	arguments	messages_ready	messages_unacknowledged	consumers
report-renderer-queue	true	false	[{"x-queue-type","classic"}]	0	0	1
report-tags-queue	true	false	[{"x-queue-type","classic"}]	0	0	1
twitter-reply-queue	true	false	[{"x-queue-type","classic"}]	0	0	1
report-analysis-queue	true	false	[{"x-queue-type","classic"}]	0	0	1
```

### Bindings

```
Listing bindings for vhost /...
source_name	destination_name	destination_kind	routing_key	arguments
	report-renderer-queue	queue	report-renderer-queue	[]
	report-tags-queue	queue	report-tags-queue	[]
	twitter-reply-queue	queue	twitter-reply-queue	[]
	report-analysis-queue	queue	report-analysis-queue	[]
cleanapp-exchange	report-renderer-queue	queue	report.analysed	[]
cleanapp-exchange	report-analysis-queue	queue	report.raw	[]
cleanapp-exchange	report-tags-queue	queue	report.raw	[]
cleanapp-exchange	twitter-reply-queue	queue	twitter.reply	[]
```

## Report Listener Services (Why Multiple Versions)

- `cleanapp_report_listener` (Go/Gin) handles `/api/v3/*` and live websocket style usage.
- `cleanapp_report_listener_v4` (Rust/Axum) handles `/api/v4/*` read-oriented endpoints and publishes OpenAPI.

### Health Checks (localhost)

```
http://127.0.0.1:9081/health	200
http://127.0.0.1:9081/api/v3/health	404
http://127.0.0.1:9081/api/v3/reports/health	200
http://127.0.0.1:9097/health	404
http://127.0.0.1:9097/api/v4/health	200
http://127.0.0.1:9097/api/v4/openapi.json	200
```

### `/api/v4` Surface Area (from OpenAPI)

- `/api/v4/brands/summary`
- `/api/v4/reports/by-brand`
- `/api/v4/reports/by-seq`
- `/api/v4/reports/points`

## High-Signal Findings

- Multiple critical services are running outside `docker compose` (no compose labels), which makes upgrades/rollbacks harder.
- RabbitMQ, MySQL, and RabbitMQ management ports are published on `0.0.0.0` (host-wide). This is risky unless the VM firewall restricts access.
- Container images in prod are pinned by registry digest, but do not expose build provenance (no OCI revision labels), so digest-to-git mapping is currently manual.

## Top 5 Optimizations (Recommended Next Upgrade Push)

1. **Build provenance and version endpoints**: add `org.opencontainers.image.revision` labels and a standard `/version` endpoint in every service (git sha, build date, config version).
2. **Deployment determinism**: bring *all* running containers under one orchestrated definition (compose + systemd), remove snowflake `docker run` containers, and document rollback procedure by image digest.
3. **Network hardening**: bind MySQL/RabbitMQ/management to localhost or internal-only, and put any admin UIs behind auth/VPN; remove default RabbitMQ credentials.
4. **Contract-driven integration**: treat `/api/v4` OpenAPI as the contract, generate clients for frontend/mobile, and add integration tests against staging (catches breaking changes before prod).
5. **Pipeline efficiency**: introduce backpressure/visibility on RabbitMQ consumers (prefetch, DLQ, metrics) and reduce LLM spend with caching/idempotency around analysis and tagging jobs.

