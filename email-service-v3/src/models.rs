use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct ReportAnalysisRow {
    pub brand_name: Option<String>,
    pub inferred_contact_emails: Option<String>,
}

#[derive(Debug, Clone)]
pub struct Brand {
    pub brand_name: String,
    pub brand_display_name: String,
}

#[derive(Debug, Clone)]
pub struct BrandEmail {
    pub email_address: String,
    pub brand_name: String,
}


