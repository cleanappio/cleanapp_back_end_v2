#!/usr/bin/env bash

compose_services_for_source_service() {
  case "$1" in
    areas-service) echo "cleanapp_areas_service" ;;
    auth-service) echo "cleanapp_auth_service" ;;
    brand-dashboard) echo "cleanapp_red_bull_dashboard" ;;
    custom-area-dashboard) echo "cleanapp_montenegro_areas cleanapp_new_york_areas cleanapp_devconnect_2025_areas cleanapp_edge_city_areas" ;;
    customer-service) echo "cleanapp_customer_service" ;;
    docker_backend) echo "cleanapp_service" ;;
    docker_pipelines) echo "cleanapp_pipelines" ;;
    email-fetcher) echo "cleanapp_email_fetcher" ;;
    email-service) echo "cleanapp_email_service" ;;
    email-service-v3) echo "cleanapp_email_service_v3" ;;
    gdpr-process-service) echo "cleanapp_gdpr_process_service" ;;
    replier-twitter) echo "cleanapp_replier_twitter" ;;
    report-analysis-backfill) echo "" ;;
    report-analyze-pipeline) echo "cleanapp_report_analyze_pipeline" ;;
    report-fast-renderer) echo "cleanapp_report_renderer_service" ;;
    report-listener) echo "cleanapp_report_listener" ;;
    report-listener-v4) echo "cleanapp_report_listener_v4" ;;
    report-ownership-service) echo "cleanapp_report_ownership_service" ;;
    report-processor) echo "cleanapp_report_processor" ;;
    report-tags) echo "cleanapp_report_tags_service" ;;
    reports-pusher) echo "cleanapp_reports_pusher" ;;
    stxn_kickoff) echo "cleanapp_stxn_kickoff" ;;
    voice-assistant-service) echo "cleanapp_voice_assistant_service" ;;
    *) return 1 ;;
  esac
}

repo_dir_for_compose_service() {
  case "$1" in
    cleanapp_auth_service) echo "auth-service" ;;
    cleanapp_customer_service) echo "customer-service" ;;
    cleanapp_report_listener) echo "report-listener" ;;
    cleanapp_areas_service) echo "areas-service" ;;
    cleanapp_email_service) echo "email-service" ;;
    cleanapp_report_ownership_service) echo "report-ownership-service" ;;
    cleanapp_report_analyze_pipeline) echo "report-analyze-pipeline" ;;
    cleanapp_report_processor) echo "report-processor" ;;
    cleanapp_gdpr_process_service) echo "gdpr-process-service" ;;
    *) return 1 ;;
  esac
}
