pub fn mask_secret(s: &str, left: usize, right: usize) -> String {
    if s.len() <= left + right { return "*".repeat(s.len()); }
    format!("{}{}{}", &s[..left], "*".repeat(s.len()-left-right), &s[s.len()-right..])
}

pub fn is_valid_email(email: &str) -> bool {
    // Simple validation; DB backfill should clean most issues
    let re = regex::Regex::new(r"^[^@\s]+@[^@\s]+\.[^@\s]+$").unwrap();
    re.is_match(email)
}


