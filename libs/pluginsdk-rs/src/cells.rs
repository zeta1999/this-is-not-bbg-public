//! Cell-grid types — JSON-compatible mirrors of `libs/pluginsdk/cells.go`.
//!
//! Field names use the same `snake_case` JSON keys; renderers don't
//! distinguish between Go and Rust producers.

use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct CellStyle {
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub fg: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub bg: String,
    #[serde(skip_serializing_if = "is_false", default)]
    pub bold: bool,
    #[serde(skip_serializing_if = "is_false", default)]
    pub italic: bool,
    #[serde(skip_serializing_if = "is_false", default)]
    pub underline: bool,
}

#[derive(Debug, Clone, Copy, Default, Serialize, Deserialize, PartialEq)]
pub struct CellAddress {
    pub row: u32,
    pub col: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnumOption {
    pub value: String,
    pub label: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChartSeries {
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub name: String,
    pub values: Vec<f64>,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub kind: String, // "line" (default), "bar", "area"
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub color: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TableColumn {
    pub header: String,
    #[serde(default, skip_serializing_if = "is_zero_u32")]
    pub width: u32,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub align: String, // "left" (default), "right", "center"
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Cell {
    pub address: CellAddress,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub style: Option<CellStyle>,
    #[serde(rename = "type")]
    pub cell_type: String,

    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub text: String,

    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub label: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub value: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub min: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub max: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub step: Option<i64>,
    #[serde(default, skip_serializing_if = "is_zero_u32")]
    pub precision: u32,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub placeholder: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub options: Vec<EnumOption>,
    #[serde(skip_serializing_if = "String::is_empty", default, rename = "search_hint")]
    pub search_hint: String,

    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub unit: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub delta: String,

    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub expression: String,

    #[serde(skip_serializing_if = "String::is_empty", default, rename = "component_id")]
    pub component_id: String,

    #[serde(skip_serializing_if = "String::is_empty", default, rename = "visible_when")]
    pub visible_when: String,
    #[serde(default, skip_serializing_if = "is_zero_u32", rename = "col_span")]
    pub col_span: u32,
    #[serde(default, skip_serializing_if = "is_zero_u32", rename = "row_span")]
    pub row_span: u32,

    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub series: Vec<ChartSeries>,

    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub columns: Vec<TableColumn>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub rows: Vec<Vec<String>>,

    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub src: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub alt: String,
    #[serde(default, skip_serializing_if = "is_zero_u32")]
    pub width: u32,
    #[serde(default, skip_serializing_if = "is_zero_u32")]
    pub height: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InputEvent {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty", rename = "screen_id")]
    pub screen_id: String,
    #[serde(default)]
    pub address: CellAddress,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub value: Option<Value>,
    #[serde(default, skip_serializing_if = "String::is_empty", rename = "job_id")]
    pub job_id: String,
}

impl InputEvent {
    /// True when the event was a job-cancel signal rather than a
    /// cell edit.
    pub fn is_cancel(&self) -> bool {
        self.kind == "cancel"
    }
}

// ---------------------------------------------------------------------------
// Constructors — small helpers mirroring the most-used Go cell builders.
// ---------------------------------------------------------------------------

pub fn text_cell(row: u32, col: u32, text: impl Into<String>) -> Cell {
    Cell {
        address: CellAddress { row, col },
        cell_type: "text".into(),
        text: text.into(),
        ..Cell::default()
    }
}

pub fn header_cell(row: u32, col: u32, text: impl Into<String>) -> Cell {
    Cell {
        address: CellAddress { row, col },
        cell_type: "text".into(),
        text: text.into(),
        style: Some(CellStyle { bold: true, ..CellStyle::default() }),
        ..Cell::default()
    }
}

pub fn number_cell(
    row: u32,
    col: u32,
    label: impl Into<String>,
    value: f64,
    precision: u32,
    unit: impl Into<String>,
) -> Cell {
    Cell {
        address: CellAddress { row, col },
        cell_type: "number".into(),
        label: label.into(),
        value: Some(Value::from(value)),
        precision,
        unit: unit.into(),
        ..Cell::default()
    }
}

pub fn decimal_input_cell(
    row: u32,
    col: u32,
    label: impl Into<String>,
    value: f64,
    precision: u32,
) -> Cell {
    Cell {
        address: CellAddress { row, col },
        cell_type: "input_decimal".into(),
        label: label.into(),
        value: Some(Value::from(value)),
        precision,
        ..Cell::default()
    }
}

pub fn integer_input_cell(
    row: u32,
    col: u32,
    label: impl Into<String>,
    value: i64,
) -> Cell {
    Cell {
        address: CellAddress { row, col },
        cell_type: "input_integer".into(),
        label: label.into(),
        value: Some(Value::from(value)),
        ..Cell::default()
    }
}

pub fn enum_input_cell(
    row: u32,
    col: u32,
    label: impl Into<String>,
    value: impl Into<String>,
    options: Vec<EnumOption>,
) -> Cell {
    Cell {
        address: CellAddress { row, col },
        cell_type: "input_enum".into(),
        label: label.into(),
        value: Some(Value::from(value.into())),
        options,
        ..Cell::default()
    }
}

pub fn image_cell(
    row: u32,
    col: u32,
    src: impl Into<String>,
    alt: impl Into<String>,
    width: u32,
    height: u32,
) -> Cell {
    Cell {
        address: CellAddress { row, col },
        cell_type: "image".into(),
        src: src.into(),
        alt: alt.into(),
        width,
        height,
        ..Cell::default()
    }
}

#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_false(v: &bool) -> bool { !*v }
#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_zero_u32(v: &u32) -> bool { *v == 0 }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn header_cell_serializes_with_bold_and_no_unused_fields() {
        let c = header_cell(0, 0, "HELLO");
        let s = serde_json::to_string(&c).unwrap();
        assert!(s.contains(r#""type":"text""#), "got {s}");
        assert!(s.contains(r#""text":"HELLO""#), "got {s}");
        assert!(s.contains(r#""bold":true"#), "got {s}");
        // Empties should be omitted.
        assert!(!s.contains("placeholder"));
        assert!(!s.contains("series"));
        assert!(!s.contains("rows"));
    }

    #[test]
    fn input_event_round_trip() {
        let raw = r#"{"kind":"edit","screen_id":"X","address":{"row":1,"col":2},"value":42}"#;
        let ev: InputEvent = serde_json::from_str(raw).unwrap();
        assert_eq!(ev.kind, "edit");
        assert_eq!(ev.address, CellAddress { row: 1, col: 2 });
        assert!(!ev.is_cancel());
        assert_eq!(ev.value.as_ref().unwrap().as_i64(), Some(42));
    }

    #[test]
    fn input_event_is_cancel() {
        let raw = r#"{"kind":"cancel","job_id":"j-1"}"#;
        let ev: InputEvent = serde_json::from_str(raw).unwrap();
        assert!(ev.is_cancel());
        assert_eq!(ev.job_id, "j-1");
    }

    #[test]
    fn image_cell_carries_notbbg_src() {
        let c = image_cell(0, 0, "NOTBBG:/tmp/x.png", "x", 640, 320);
        let s = serde_json::to_string(&c).unwrap();
        assert!(s.contains(r#""type":"image""#));
        assert!(s.contains(r#""src":"NOTBBG:/tmp/x.png""#));
        assert!(s.contains(r#""width":640"#));
    }
}
