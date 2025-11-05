use sqlx::MySqlPool;
use std::sync::Arc;
use crate::rabbitmq::TagEventPublisher;

#[derive(Clone)]
pub struct AppState {
    pub pool: MySqlPool,
    pub publisher: Option<Arc<TagEventPublisher>>,
}

