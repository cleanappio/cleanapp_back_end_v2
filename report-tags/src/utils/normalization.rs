use thiserror::Error;

#[derive(Error, Debug)]
pub enum TagError {
    #[error("Tag is too short (minimum 1 character)")]
    TooShort,
    #[error("Tag is too long (maximum 64 characters)")]
    TooLong,
    #[error("Tag contains invalid characters")]
    InvalidCharacters,
}

pub fn normalize_tag(input: &str) -> Result<(String, String), TagError> {
    let trimmed = input.trim();
    
    // Remove leading # if present
    let without_hash = trimmed.strip_prefix('#').unwrap_or(trimmed);
    
    // Unicode NFKC normalization
    let normalized = unicode_normalization::UnicodeNormalization::nfkc(without_hash);
    let canonical = normalized.collect::<String>().to_lowercase();
    
    // Validate length
    if canonical.is_empty() {
        return Err(TagError::TooShort);
    }
    
    if canonical.len() > 64 {
        return Err(TagError::TooLong);
    }
    
    // Basic character validation - allow letters, numbers, spaces, and common punctuation
    if !canonical.chars().all(|c| c.is_alphanumeric() || c.is_whitespace() || ".-_".contains(c)) {
        return Err(TagError::InvalidCharacters);
    }
    
    Ok((canonical, without_hash.to_string())) // (canonical_name, display_name)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_normalize_tag_basic() {
        assert_eq!(normalize_tag("Beach").unwrap(), ("beach".to_string(), "Beach".to_string()));
        assert_eq!(normalize_tag("CLEANUP").unwrap(), ("cleanup".to_string(), "CLEANUP".to_string()));
        assert_eq!(normalize_tag("plastic waste").unwrap(), ("plastic waste".to_string(), "plastic waste".to_string()));
    }

    #[test]
    fn test_normalize_tag_with_hash() {
        assert_eq!(normalize_tag("#Beach").unwrap(), ("beach".to_string(), "Beach".to_string()));
        assert_eq!(normalize_tag("#cleanup").unwrap(), ("cleanup".to_string(), "cleanup".to_string()));
    }

    #[test]
    fn test_normalize_tag_unicode() {
        assert_eq!(normalize_tag("café").unwrap(), ("cafe".to_string(), "café".to_string()));
        assert_eq!(normalize_tag("naïve").unwrap(), ("naive".to_string(), "naïve".to_string()));
    }

    #[test]
    fn test_normalize_tag_whitespace() {
        assert_eq!(normalize_tag("  Beach  ").unwrap(), ("beach".to_string(), "Beach".to_string()));
        assert_eq!(normalize_tag("\t\nBeach\t\n").unwrap(), ("beach".to_string(), "Beach".to_string()));
    }

    #[test]
    fn test_normalize_tag_errors() {
        assert!(matches!(normalize_tag(""), Err(TagError::TooShort)));
        assert!(matches!(normalize_tag("   "), Err(TagError::TooShort)));
        assert!(matches!(normalize_tag(&"a".repeat(65)), Err(TagError::TooLong)));
    }

    #[test]
    fn test_normalize_tag_special_chars() {
        assert_eq!(normalize_tag("beach-cleanup").unwrap(), ("beach-cleanup".to_string(), "beach-cleanup".to_string()));
        assert_eq!(normalize_tag("beach.cleanup").unwrap(), ("beach.cleanup".to_string(), "beach.cleanup".to_string()));
        assert_eq!(normalize_tag("beach_cleanup").unwrap(), ("beach_cleanup".to_string(), "beach_cleanup".to_string()));
    }
}
