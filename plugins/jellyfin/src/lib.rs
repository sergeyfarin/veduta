// SPDX-License-Identifier: Apache-2.0

use extism_pdk::{Error, FnResult, Json, plugin_fn};
use serde::Deserialize;
use serde_json::{Value, json};
use std::collections::BTreeMap;
use veduta_sdk::{
    AssetRequest, Block, Document, Hints, HttpRequest, Image, MediaItem, MetricItem, Notice,
    Signal, Status, Transform, asset_ref, http,
};

const JSON_PROFILE: &str = "application/json; profile=\"CamelCase\"";

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Item {
    #[serde(alias = "Id")]
    id: String,
    #[serde(alias = "Name")]
    name: String,
    #[serde(default, alias = "ProductionYear")]
    production_year: Option<u16>,
    #[serde(default, alias = "ImageTags")]
    image_tags: BTreeMap<String, String>,
}

/// The `/Items` envelope. The recently-added query reads `items`; the per-type
/// count queries read `total_record_count` and ignore the (single) item.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ItemsResponse {
    #[serde(default, alias = "Items")]
    items: Vec<Item>,
    #[serde(default, alias = "TotalRecordCount")]
    total_record_count: u64,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Session {
    #[serde(default, alias = "NowPlayingItem")]
    now_playing_item: Option<Value>,
}

fn query(entries: &[(&str, String)]) -> BTreeMap<String, String> {
    entries
        .iter()
        .map(|(key, value)| ((*key).to_string(), value.clone()))
        .collect()
}

fn get<T: for<'de> Deserialize<'de>>(path: &str, query: BTreeMap<String, String>) -> FnResult<T> {
    let response = http(HttpRequest {
        slot: "server".into(),
        method: "GET".into(),
        path: path.into(),
        query,
        headers: BTreeMap::from([("Accept".into(), JSON_PROFILE.into())]),
        body: None,
    })?;
    if !(200..300).contains(&response.status) {
        return Err(
            Error::msg(format!("Jellyfin {path} returned HTTP {}", response.status)).into(),
        );
    }
    let body = response
        .body
        .ok_or_else(|| Error::msg(format!("Jellyfin {path} returned no JSON body")))?;
    Ok(serde_json::from_value(body)?)
}

/// A library-wide count for one item type. `/Items` accepts an optional `userId`;
/// omitting it counts the whole library and needs only the connection's API key -
/// a Jellyfin API key has no associated user, so anything scoped to "the current
/// user" (`/Users/Me`, `/Items/Latest` without a userId) is unavailable to us.
fn count(item_type: &str) -> FnResult<u64> {
    let result: ItemsResponse = get(
        "/Items",
        query(&[
            ("recursive", "true".into()),
            ("includeItemTypes", item_type.into()),
            ("limit", "1".into()),
        ]),
    )?;
    Ok(result.total_record_count)
}

#[plugin_fn]
pub fn invoke(Json(input): Json<veduta_sdk::Invocation>) -> FnResult<Json<Document>> {
    if input.is_validation() {
        return Ok(Json(Document::validation()));
    }
    if input.operation != "recently-added" {
        return Err(Error::msg(format!("unknown operation {}", input.operation)).into());
    }
    let limit = input
        .params
        .get("limit")
        .and_then(Value::as_u64)
        .unwrap_or(5)
        .clamp(1, 24);

    // Recently added, newest first, movies and shows together. One `/Items` query
    // with an explicit sort replaces the removed `/Users/{userId}/Items` and the
    // user-scoped `/Items/Latest` - see docs/spikes/s2-upstream-reality-check.md.
    let recent: ItemsResponse = get(
        "/Items",
        query(&[
            ("recursive", "true".into()),
            ("includeItemTypes", "Movie,Series".into()),
            ("sortBy", "DateCreated".into()),
            ("sortOrder", "Descending".into()),
            ("limit", limit.to_string()),
            ("fields", "ProductionYear".into()),
            ("imageTypeLimit", "1".into()),
            ("enableImageTypes", "Primary".into()),
            ("enableImages", "true".into()),
        ]),
    )?;
    let movies = count("Movie")?;
    let shows = count("Series")?;
    let sessions: Vec<Session> = get("/Sessions", BTreeMap::new())?;
    let active_streams = sessions
        .iter()
        .filter(|session| session.now_playing_item.is_some())
        .count() as u64;

    let mut posters = Vec::new();
    let mut missing_images = 0;
    for item in recent.items.into_iter().take(limit as usize) {
        if !item.image_tags.contains_key("Primary") && !item.image_tags.contains_key("primary") {
            missing_images += 1;
            continue;
        }
        let reference = asset_ref(AssetRequest {
            slot: "server".into(),
            path: format!("/Items/{}/Images/Primary", item.id),
            query: BTreeMap::new(),
            transform: Some(Transform {
                width: Some(320),
                format: None,
            }),
        })?;
        posters.push(MediaItem {
            image: Image {
                reference,
                alt: Some(format!("{} poster", item.name)),
                aspect: Some("2:3".into()),
                blurhash: None,
            },
            id: Some(item.id),
            title: Some(item.name),
            subtitle: item.production_year.map(|year| year.to_string()),
            badge: None,
            level: None,
            link: None,
            timestamp: None,
        });
    }

    let mut document = Document::new("Jellyfin");
    document.status = Some(Status {
        level: "ok".into(),
        text: Some("Online".into()),
        since: None,
    });
    document.hints = Some(Hints { ttl_seconds: 300 });
    document.blocks = vec![
        Block::PosterGrid {
            items: posters,
            title: None,
            emphasis: None,
            columns: Some(5),
            empty: Some("No recent items with posters".into()),
        },
        Block::Metrics {
            title: None,
            emphasis: None,
            columns: None,
            items: vec![
                metric("Movies", movies, None),
                metric("Shows", shows, None),
                metric("Streams", active_streams, Some("ok")),
            ],
        },
    ];
    document.signals = BTreeMap::from([
        ("movies".into(), signal(movies, None)),
        ("shows".into(), signal(shows, None)),
        ("active_streams".into(), signal(active_streams, Some("ok"))),
    ]);
    if missing_images > 0 {
        document.notices.push(Notice {
            level: "info".into(),
            message: format!("{missing_images} recent items had no primary image"),
        });
    }
    Ok(Json(document))
}

fn metric(label: &str, value: u64, level: Option<&str>) -> MetricItem {
    MetricItem {
        label: label.into(),
        value: json!(value),
        format: Some("number".into()),
        unit: None,
        level: level.map(str::to_string),
        icon: None,
        link: None,
    }
}

fn signal(value: u64, level: Option<&str>) -> Signal {
    Signal {
        value: json!(value),
        unit: Some("count".into()),
        level: level.map(str::to_string),
    }
}
