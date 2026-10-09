use chrono::{DateTime, Duration, Utc};
use serde_json::Value;

pub const MAX_AGE_HOURS: i64 = 48;
pub const SEARCH_QUERIES: &str = "pothole,illegal dumping,blocked sidewalk,broken streetlight,water leak,fallen tree,broken link,404 error,app crashing,login broken,sync failed,accessibility issue,confusing interface,unusable app";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IssueKind {
    Physical,
    Digital,
}

pub fn fresh(created: Option<&str>, now: DateTime<Utc>) -> bool {
    created
        .and_then(|s| DateTime::parse_from_rfc3339(s).ok())
        .is_some_and(|t| {
            t >= now - Duration::hours(MAX_AGE_HOURS) && t <= now + Duration::minutes(5)
        })
}

// Only the author's own media counts: quote-post images and link-preview cards
// do not demonstrate that this person observed a physical problem.
pub fn images(embed: &Value) -> Vec<&Value> {
    let media = embed.get("media").unwrap_or(embed);
    media
        .get("images")
        .and_then(Value::as_array)
        .map(|v| v.iter().collect())
        .unwrap_or_default()
}

pub fn image_urls(embed: &Value, did: &str) -> Vec<String> {
    images(embed)
        .iter()
        .filter_map(|img| {
            if let Some(url) = img
                .get("fullsize")
                .or_else(|| img.get("thumb"))
                .and_then(Value::as_str)
            {
                return Some(url.to_owned());
            }
            let cid = img.pointer("/image/ref/$link")?.as_str()?;
            Some(format!(
                "https://cdn.bsky.app/img/feed_fullsize/plain/{did}/{cid}@jpeg"
            ))
        })
        .take(4)
        .collect()
}

pub fn kind(text: &str, has_images: bool) -> Option<IssueKind> {
    let text = text.to_lowercase().replace("\\n", "\n");
    if [
        "#emvalert",
        "resources:",
        "status: complete",
        "status: resolved",
        "sponsored content",
        "giveaway",
        "follow for follow",
        "crypto airdrop",
    ]
    .iter()
    .any(|s| text.contains(s))
    {
        return None;
    }
    let digital_context = [
        "app",
        "website",
        "site",
        "software",
        "browser",
        "login",
        "log in",
        "sign in",
        "checkout",
        "interface",
        " ux",
        " ui",
        "link",
        "button",
        "screen reader",
        "api",
        "discord",
        "spotify",
        "bluesky",
        "android",
        "ios",
        "windows",
        "github",
        "google",
        "microsoft",
        "chatgpt",
        "youtube",
        "instagram",
        "facebook",
        "steam",
    ]
    .iter()
    .any(|s| text.contains(s));
    let failure = [
        "broken",
        "bug",
        "crash",
        "404",
        "not found",
        "doesn't work",
        "does not work",
        "not working",
        "can't",
        "cannot",
        "won't",
        "fails",
        "failed",
        "error",
        "frustrat",
        "confus",
        "unusable",
        "inaccessible",
        "keeps freezing",
        "laggy",
        "battery drain",
        "sync",
        "hate",
        "annoy",
        "dark mode",
        "hard to use",
    ]
    .iter()
    .any(|s| text.contains(s));
    if digital_context && failure {
        return Some(IssueKind::Digital);
    }
    if has_images
        && [
            "pothole",
            "litter",
            "dumping",
            "dumped",
            "trash",
            "rubbish",
            "garbage",
            "blocked sidewalk",
            "blocked pavement",
            "blocked footpath",
            "broken sidewalk",
            "broken pavement",
            "inaccessible sidewalk",
            "wheelchair access",
            "streetlight",
            "street light",
            "traffic light",
            "water leak",
            "burst pipe",
            "sewage",
            "flood",
            "fallen tree",
            "tree down",
            "downed tree",
            "road hazard",
            "damaged road",
            "pollution",
            "graffiti",
            "broken bench",
            "damaged sign",
            "blocked drain",
            "overflowing bin",
        ]
        .iter()
        .any(|s| text.contains(s))
    {
        return Some(IssueKind::Physical);
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn admits_diverse_photo_issues_and_fresh_digital_failures() {
        for text in [
            "Pothole on Main St",
            "Rubbish dumped near the station",
            "Blocked sidewalk, wheelchair access impossible",
            "Streetlight broken",
            "Fallen tree blocking the road",
        ] {
            assert_eq!(kind(text, true), Some(IssueKind::Physical));
            assert_eq!(kind(text, false), None);
        }
        for text in [
            "This website's broken link returns 404",
            "Discord keeps crashing",
            "The checkout UI is frustrating and confusing",
            "This app doesn't work with a screen reader",
        ] {
            assert_eq!(kind(text, false), Some(IssueKind::Digital));
        }
        for text in [
            "Tree Down Main St\\nStatus: Responding\\nResources: 1 #EMVAlert",
            "Tree Down Main St\nStatus: Complete",
            "This political campaign is broken",
            "Lovely tree photo",
            "New software giveaway",
        ] {
            assert_eq!(kind(text, true), None);
        }
    }
    #[test]
    fn rejects_old_missing_and_future_timestamps() {
        let now = Utc::now();
        assert!(fresh(Some(&now.to_rfc3339()), now));
        assert!(!fresh(Some(&(now - Duration::hours(49)).to_rfc3339()), now));
        assert!(!fresh(Some(&(now + Duration::hours(1)).to_rfc3339()), now));
        assert!(!fresh(None, now));
    }
    #[test]
    fn handles_photos_with_quotes_but_not_quoted_or_link_preview_images() {
        let image = json!({"image":{"ref":{"$link":"cid"}}});
        let embed = json!({"media":{"images":[image]},"record":{}});
        assert_eq!(
            image_urls(&embed, "did:plc:test"),
            vec!["https://cdn.bsky.app/img/feed_fullsize/plain/did:plc:test/cid@jpeg"]
        );
        assert!(images(&json!({"external":{"thumb":"preview"}})).is_empty());
        assert!(images(&json!({"record":{"images":[{}]}})).is_empty());
    }
}
