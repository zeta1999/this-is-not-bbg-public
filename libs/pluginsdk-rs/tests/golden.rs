//! Golden-output integration tests — assert the wire shape the
//! Rust SDK emits matches what the Go SDK / TUI / desktop expects.
//!
//! If you change a cell field name or add a new omitempty default,
//! these tests will catch it before the change reaches a renderer.

use notbbg_pluginsdk::cells::{
    decimal_input_cell, enum_input_cell, header_cell, image_cell, integer_input_cell,
    number_cell, text_cell, EnumOption,
};
use serde_json::Value;

#[test]
fn header_cell_layout_matches_go_sdk() {
    let c = header_cell(0, 0, "TITLE");
    let v: Value = serde_json::to_value(&c).unwrap();
    assert_eq!(v["type"], "text");
    assert_eq!(v["text"], "TITLE");
    assert_eq!(v["address"]["row"], 0);
    assert_eq!(v["address"]["col"], 0);
    assert_eq!(v["style"]["bold"], true);
    // Empty fields must be absent.
    assert!(v.get("placeholder").is_none());
    assert!(v.get("series").is_none());
}

#[test]
fn number_cell_includes_unit_and_precision() {
    let c = number_cell(2, 1, "PnL", -123.45, 2, "$");
    let v: Value = serde_json::to_value(&c).unwrap();
    assert_eq!(v["type"], "number");
    assert_eq!(v["label"], "PnL");
    assert_eq!(v["value"], -123.45);
    assert_eq!(v["precision"], 2);
    assert_eq!(v["unit"], "$");
}

#[test]
fn enum_input_cell_carries_options() {
    let c = enum_input_cell(
        0,
        2,
        "Side",
        "buy",
        vec![
            EnumOption { value: "buy".into(), label: "Buy".into() },
            EnumOption { value: "sell".into(), label: "Sell".into() },
        ],
    );
    let v: Value = serde_json::to_value(&c).unwrap();
    assert_eq!(v["type"], "input_enum");
    assert_eq!(v["value"], "buy");
    assert_eq!(v["options"][0]["value"], "buy");
    assert_eq!(v["options"][1]["label"], "Sell");
}

#[test]
fn decimal_and_integer_input_cells_have_distinct_types() {
    let d = decimal_input_cell(0, 0, "vol", 0.2, 4);
    let i = integer_input_cell(1, 0, "ticks", 100);
    let dv: Value = serde_json::to_value(&d).unwrap();
    let iv: Value = serde_json::to_value(&i).unwrap();
    assert_eq!(dv["type"], "input_decimal");
    assert_eq!(iv["type"], "input_integer");
    assert_eq!(dv["precision"], 4);
    // input_integer has no precision field by convention.
    assert!(iv.get("precision").is_none());
}

#[test]
fn image_cell_round_trips() {
    let c = image_cell(2, 0, "NOTBBG:/tmp/x.png", "alt", 640, 320);
    let s = serde_json::to_string(&c).unwrap();
    let back: notbbg_pluginsdk::cells::Cell = serde_json::from_str(&s).unwrap();
    assert_eq!(back.cell_type, "image");
    assert_eq!(back.src, "NOTBBG:/tmp/x.png");
    assert_eq!(back.alt, "alt");
    assert_eq!(back.width, 640);
    assert_eq!(back.height, 320);
}

#[test]
fn text_cell_omits_styling_when_default() {
    let c = text_cell(0, 0, "hi");
    let v: Value = serde_json::to_value(&c).unwrap();
    assert!(v.get("style").is_none(), "default-styled text cell should omit `style`: {v}");
}
