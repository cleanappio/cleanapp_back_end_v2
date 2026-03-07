use std::env;
use std::fmt::Display;
use std::str::FromStr;

pub fn string(key: &str, default: &str) -> String {
    env::var(key).unwrap_or_else(|_| default.to_string())
}

pub fn optional(key: &str) -> Option<String> {
    env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

pub fn list(key: &str, default: &str) -> Vec<String> {
    let raw = optional(key).unwrap_or_else(|| default.to_string());
    raw.split(',')
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .collect()
}

pub fn required(key: &str) -> String {
    match optional(key) {
        Some(value) => value,
        None => panic!("{key} environment variable is required"),
    }
}

pub fn bool(key: &str, default: bool) -> bool {
    match env::var(key) {
        Ok(value) => matches!(
            value.trim().to_lowercase().as_str(),
            "1" | "true" | "yes" | "on"
        ),
        Err(_) => default,
    }
}

pub fn parse<T>(key: &str, default: &str) -> T
where
    T: FromStr,
    <T as FromStr>::Err: Display,
{
    let raw = string(key, default);
    raw.parse::<T>()
        .unwrap_or_else(|err| panic!("{key} parse error: {err}"))
}
