// SPDX-License-Identifier: Apache-2.0

//! Typed Widget Document builders and capability-broker bindings for Veduta plugins.

use extism_pdk::{Error, FnResult, Json, host_fn};
use serde::{Deserialize, Serialize, de::DeserializeOwned};
use serde_json::Value;
use std::collections::BTreeMap;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Invocation {
    pub operation: String,
    #[serde(default)]
    pub params: Value,
    pub now: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Document {
    pub schema_version: u8,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub subtitle: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub link: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<Status>,
    pub blocks: Vec<Block>,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub signals: BTreeMap<String, Signal>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub notices: Vec<Notice>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub hints: Option<Hints>,
}

impl Document {
    #[must_use]
    pub fn new(title: impl Into<String>) -> Self {
        Self {
            schema_version: 1,
            title: Some(title.into()),
            subtitle: None,
            link: None,
            status: None,
            blocks: Vec::new(),
            signals: BTreeMap::new(),
            notices: Vec::new(),
            hints: None,
        }
    }

    #[must_use]
    pub fn block(mut self, block: Block) -> Self {
        self.blocks.push(block);
        self
    }

    #[must_use]
    pub fn signal(mut self, name: impl Into<String>, signal: Signal) -> Self {
        self.signals.insert(name.into(), signal);
        self
    }
}

#[derive(Debug, Serialize)]
#[serde(tag = "type", rename_all = "kebab-case")]
pub enum Block {
    Text {
        content: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        level: Option<String>,
    },
    Markdown {
        content: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        level: Option<String>,
    },
    Metrics {
        items: Vec<MetricItem>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        columns: Option<u8>,
    },
    KeyValue {
        items: Vec<MetricItem>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
    },
    Progress {
        items: Vec<ProgressItem>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
    },
    Status {
        items: Vec<StatusItem>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
    },
    List {
        items: Vec<ListItem>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        empty: Option<String>,
    },
    Image {
        items: Vec<MediaItem>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        columns: Option<u8>,
        #[serde(skip_serializing_if = "Option::is_none")]
        empty: Option<String>,
    },
    ImageGrid {
        items: Vec<MediaItem>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        columns: Option<u8>,
        #[serde(skip_serializing_if = "Option::is_none")]
        empty: Option<String>,
    },
    PosterGrid {
        items: Vec<MediaItem>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        columns: Option<u8>,
        #[serde(skip_serializing_if = "Option::is_none")]
        empty: Option<String>,
    },
    Table {
        columns: Vec<TableColumn>,
        rows: Vec<BTreeMap<String, Value>>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
    },
    Actions {
        actions: Vec<Action>,
        #[serde(skip_serializing_if = "Option::is_none")]
        title: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        emphasis: Option<String>,
    },
}

#[derive(Debug, Serialize)]
pub struct MetricItem {
    pub label: String,
    pub value: Value,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub format: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub unit: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub icon: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub link: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ProgressItem {
    pub label: String,
    pub progress: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub value: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub format: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct StatusItem {
    pub label: String,
    pub level: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub since: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub link: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ListItem {
    pub title: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub subtitle: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub value: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub format: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub icon: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub image: Option<Image>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub link: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timestamp: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct Image {
    #[serde(rename = "ref")]
    pub reference: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub alt: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub aspect: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub blurhash: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct MediaItem {
    pub image: Image,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub subtitle: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub badge: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub link: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timestamp: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct TableColumn {
    pub key: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub label: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub format: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub align: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct Action {
    pub id: String,
    pub label: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub icon: Option<String>,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub confirm: bool,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub danger: bool,
}

#[derive(Debug, Serialize)]
pub struct Status {
    pub level: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub since: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct Notice {
    pub level: String,
    pub message: String,
}

#[derive(Debug, Serialize)]
pub struct Signal {
    pub value: Value,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub unit: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Hints {
    pub ttl_seconds: u32,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct HttpRequest {
    pub slot: String,
    pub method: String,
    pub path: String,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub query: BTreeMap<String, String>,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub headers: BTreeMap<String, String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub body: Option<Value>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct HttpResponse {
    pub status: u16,
    #[serde(default)]
    pub headers: BTreeMap<String, Vec<String>>,
    pub body: Option<Value>,
    pub body_base64: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AssetRequest {
    pub slot: String,
    pub path: String,
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub query: BTreeMap<String, String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub transform: Option<Transform>,
}

#[derive(Debug, Serialize)]
pub struct Transform {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub width: Option<u16>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub format: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct HostError {
    pub code: String,
    pub message: String,
}

#[derive(Debug, Deserialize)]
struct HostResult<T> {
    ok: bool,
    value: Option<T>,
    error: Option<HostError>,
}

#[host_fn("extism:host/user")]
extern "ExtismHost" {
    fn veduta_http(input: Json<HttpRequest>) -> Json<HostResult<HttpResponse>>;
    fn veduta_cache_get(input: String) -> Json<HostResult<Value>>;
    fn veduta_cache_put(input: Json<Value>) -> Json<HostResult<Value>>;
    fn veduta_asset_ref(input: Json<AssetRequest>) -> Json<HostResult<String>>;
    fn veduta_log(input: Json<Value>) -> Json<HostResult<Value>>;
}

fn value<T>(result: HostResult<T>) -> FnResult<Option<T>> {
    if result.ok {
        return Ok(result.value);
    }
    let error = result.error.unwrap_or(HostError {
        code: "invalid_response".into(),
        message: "host call failed without an error".into(),
    });
    Err(Error::msg(format!("{}: {}", error.code, error.message)).into())
}

pub fn http(request: HttpRequest) -> FnResult<HttpResponse> {
    let result = unsafe { veduta_http(Json(request))? }.0;
    Ok(value(result)?.ok_or_else(|| Error::msg("host returned no HTTP response"))?)
}

pub fn cache_get<T: DeserializeOwned>(key: impl Into<String>) -> FnResult<Option<T>> {
    let result = unsafe { veduta_cache_get(key.into())? }.0;
    Ok(value(result)?
        .map(serde_json::from_value)
        .transpose()
        .map_err(Error::from)?)
}

pub fn cache_put<T: Serialize>(
    key: impl Into<String>,
    value_to_store: T,
    ttl_seconds: u32,
) -> FnResult<()> {
    let request =
        serde_json::json!({"key": key.into(), "value": value_to_store, "ttlSeconds": ttl_seconds});
    let result = unsafe { veduta_cache_put(Json(request))? }.0;
    value(result).map(|_| ())
}

pub fn asset_ref(request: AssetRequest) -> FnResult<String> {
    let result = unsafe { veduta_asset_ref(Json(request))? }.0;
    Ok(value(result)?.ok_or_else(|| Error::msg("host returned no asset reference"))?)
}

pub fn log(level: &str, message: &str, fields: Value) -> FnResult<()> {
    let request = serde_json::json!({"level": level, "msg": message, "fields": fields});
    let result = unsafe { veduta_log(Json(request))? }.0;
    value(result).map(|_| ())
}

#[cfg(test)]
mod tests {
    use super::{Block, Document, Signal};
    use serde_json::json;

    #[test]
    fn document_uses_the_v1_wire_shape() {
        let document = Document::new("Hello")
            .block(Block::Text {
                content: "world".into(),
                title: None,
                emphasis: None,
                level: None,
            })
            .signal(
                "ready",
                Signal {
                    value: json!(true),
                    unit: Some("boolean".into()),
                    level: Some("ok".into()),
                },
            );

        assert_eq!(
            serde_json::to_value(document).unwrap(),
            json!({
                "schemaVersion": 1,
                "title": "Hello",
                "blocks": [{"type": "text", "content": "world"}],
                "signals": {"ready": {"value": true, "unit": "boolean", "level": "ok"}}
            })
        );
    }
}
