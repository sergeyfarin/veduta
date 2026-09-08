// SPDX-License-Identifier: Apache-2.0

use extism_pdk::{FnResult, Json, plugin_fn};
use serde_json::json;
use veduta_sdk::{Block, Document, Invocation, Signal};

#[plugin_fn]
pub fn invoke(Json(input): Json<Invocation>) -> FnResult<Json<Document>> {
    let document = Document::new("Hello from WASM")
        .block(Block::Text {
            content: format!("Operation `{}` ran at {}.", input.operation, input.now),
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
    Ok(Json(document))
}
