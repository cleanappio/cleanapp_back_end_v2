use sqlx::MySqlPool;

#[derive(Clone)]
pub struct AppState {
    pub pool: MySqlPool,
    // TODO: Add tag event publisher back when we have consumers for tag.added events
    // pub publisher: Option<Arc<TagEventPublisher>>,
}

