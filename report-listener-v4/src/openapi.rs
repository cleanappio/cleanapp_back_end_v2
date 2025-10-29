use utoipa::OpenApi;
use utoipa_swagger_ui::SwaggerUi;

use crate::models::{BrandSummaryItem, ReportBatch, ReportWithAnalysis, Report, ReportPoint};

#[derive(OpenApi)]
#[openapi(
    paths(
        crate::get_brands_summary,
        crate::get_reports_by_brand,
        crate::get_report_points,
        crate::get_report_by_seq,
    ),
    components(
        schemas(BrandSummaryItem, ReportBatch, ReportWithAnalysis, Report, ReportPoint)
    ),
    tags(
        (name = "report-listener-v4", description = "Brand summaries and reports by brand")
    )
)]
pub struct ApiDoc;

pub fn routes() -> utoipa_swagger_ui::SwaggerUi {
    let openapi = ApiDoc::openapi();
    SwaggerUi::new("/api/v4/docs").url("/api/v4/openapi.json", openapi)
}


