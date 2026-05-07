// notbbg-pluginsdk-cpp — C++17 port of libs/pluginsdk (Go).
//
// Same JSON-per-line stdin/stdout protocol, same cell-grid layout.
// Header-only-ish: most logic is inline; the small amount of state
// (cell builders, escaped-string helper) lives in pluginsdk.cpp.
//
// Design constraint (project_oss_plugin_isolation.md): no external
// runtime deps. JSON serialisation is hand-rolled for the cell
// shapes the SDK emits; inbound payloads are exposed as raw
// std::string (the plugin can pull in nlohmann or anything else
// to parse them — the SDK doesn't take a position).

#pragma once

#include <cstdint>
#include <iostream>
#include <sstream>
#include <string>
#include <utility>
#include <vector>

namespace notbbg {

// CellAddress identifies a cell position in the grid.
struct CellAddress {
    std::uint32_t row{0};
    std::uint32_t col{0};
};

// CellStyle controls text appearance.
struct CellStyle {
    std::string fg;
    std::string bg;
    bool bold{false};
    bool italic{false};
    bool underline{false};

    bool empty() const noexcept {
        return fg.empty() && bg.empty() && !bold && !italic && !underline;
    }
};

// Cell — a minimal, serializable subset of the Go SDK's Cell type.
// Plugins set fields directly; the JSON serializer omits empties.
struct Cell {
    CellAddress address{};
    CellStyle style{};
    std::string type;          // "text", "header", "number", "input_decimal", ...

    // text
    std::string text;

    // input_*
    std::string label;
    std::string value_str;     // string value (for enum / string input)
    double      value_num{0};  // numeric value (for decimal / integer / number)
    bool        has_num{false};
    std::uint32_t precision{0};
    std::string placeholder;

    // number
    std::string unit;
    std::string delta;         // "up", "down", ""

    // image
    std::string src;
    std::string alt;
    std::uint32_t width{0};
    std::uint32_t height{0};
};

// Inbound message envelope. Payload stays raw — plugin parses it
// however it likes (nlohmann, RapidJSON, hand-rolled).
struct Message {
    std::string topic;
    std::string payload_raw;   // the un-decoded JSON payload value
};

// JSON-escape a string into out. Internal but exposed for tests.
void json_escape(const std::string& s, std::string& out);

// Serialize a single Cell to its JSON object form.
std::string cell_to_json(const Cell& c);

// Cell builders (sugar — use directly or build by hand).
inline Cell text_cell(std::uint32_t row, std::uint32_t col, std::string text) {
    Cell c;
    c.address = {row, col};
    c.type = "text";
    c.text = std::move(text);
    return c;
}

inline Cell header_cell(std::uint32_t row, std::uint32_t col, std::string text) {
    Cell c = text_cell(row, col, std::move(text));
    c.style.bold = true;
    return c;
}

inline Cell number_cell(std::uint32_t row, std::uint32_t col,
                        std::string label, double value,
                        std::uint32_t precision, std::string unit) {
    Cell c;
    c.address = {row, col};
    c.type = "number";
    c.label = std::move(label);
    c.value_num = value;
    c.has_num = true;
    c.precision = precision;
    c.unit = std::move(unit);
    return c;
}

inline Cell decimal_input_cell(std::uint32_t row, std::uint32_t col,
                               std::string label, double value,
                               std::uint32_t precision) {
    Cell c;
    c.address = {row, col};
    c.type = "input_decimal";
    c.label = std::move(label);
    c.value_num = value;
    c.has_num = true;
    c.precision = precision;
    return c;
}

// Plugin — owns stdin/stdout framing.
class Plugin {
public:
    explicit Plugin(std::string screen_topic) : screen_topic_(std::move(screen_topic)) {}

    // Block waiting for the next inbound frame; returns false on EOF.
    bool read(Message& out);

    // Emit a CellGridUpdate to the screen topic.
    void update_cell_grid(const std::string& screen_id,
                          const std::vector<Cell>& cells,
                          bool full_replace = true);

    // Emit a raw {Topic, Payload} envelope. Caller supplies
    // payload_json_value (must be a valid JSON value).
    void publish_raw(const std::string& topic, const std::string& payload_json_value);

    // Main loop: call handler(msg) for each inbound message.
    template <typename F>
    void run(F&& handler) {
        Message m;
        while (read(m)) {
            handler(*this, m);
        }
    }

    const std::string& screen_topic() const noexcept { return screen_topic_; }

private:
    std::string screen_topic_;
};

}  // namespace notbbg
