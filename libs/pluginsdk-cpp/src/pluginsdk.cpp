// notbbg-pluginsdk-cpp implementation.

#include "notbbg/pluginsdk.hpp"

#include <cstdio>
#include <cstring>
#include <iomanip>

namespace notbbg {

void json_escape(const std::string& s, std::string& out) {
    for (char c : s) {
        switch (c) {
            case '"':  out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\b': out += "\\b";  break;
            case '\f': out += "\\f";  break;
            case '\n': out += "\\n";  break;
            case '\r': out += "\\r";  break;
            case '\t': out += "\\t";  break;
            default:
                if (static_cast<unsigned char>(c) < 0x20) {
                    char buf[8];
                    std::snprintf(buf, sizeof(buf), "\\u%04x", c);
                    out += buf;
                } else {
                    out += c;
                }
        }
    }
}

namespace {

void append_string(std::string& dst, const std::string& key, const std::string& val) {
    if (val.empty()) return;
    if (!dst.empty() && dst.back() != '{') dst += ',';
    dst += '"';
    dst += key;
    dst += "\":\"";
    json_escape(val, dst);
    dst += '"';
}

[[maybe_unused]] void append_uint(std::string& dst, const std::string& key, std::uint32_t v) {
    if (v == 0) return;
    if (!dst.empty() && dst.back() != '{') dst += ',';
    dst += '"';
    dst += key;
    dst += "\":";
    dst += std::to_string(v);
}

void append_bool(std::string& dst, const std::string& key, bool v) {
    if (!v) return;
    if (!dst.empty() && dst.back() != '{') dst += ',';
    dst += '"';
    dst += key;
    dst += "\":true";
}

[[maybe_unused]] void append_double(std::string& dst, const std::string& key, double v, bool always = false) {
    if (!always && v == 0.0) return;
    if (!dst.empty() && dst.back() != '{') dst += ',';
    dst += '"';
    dst += key;
    dst += "\":";
    char buf[32];
    std::snprintf(buf, sizeof(buf), "%.17g", v);
    dst += buf;
}

std::string style_to_json(const CellStyle& s) {
    if (s.empty()) return {};
    std::string out = "{";
    append_string(out, "fg", s.fg);
    append_string(out, "bg", s.bg);
    append_bool(out, "bold", s.bold);
    append_bool(out, "italic", s.italic);
    append_bool(out, "underline", s.underline);
    out += '}';
    return out;
}

}  // namespace

std::string cell_to_json(const Cell& c) {
    std::string out = "{";

    // address (always present)
    out += "\"address\":{\"row\":";
    out += std::to_string(c.address.row);
    out += ",\"col\":";
    out += std::to_string(c.address.col);
    out += '}';

    if (!c.style.empty()) {
        out += ",\"style\":";
        out += style_to_json(c.style);
    }

    if (!c.type.empty()) {
        out += ",\"type\":\"";
        json_escape(c.type, out);
        out += '"';
    }

    if (!c.text.empty()) {
        out += ",\"text\":\"";
        json_escape(c.text, out);
        out += '"';
    }
    if (!c.label.empty()) {
        out += ",\"label\":\"";
        json_escape(c.label, out);
        out += '"';
    }
    if (c.has_num) {
        out += ",\"value\":";
        char buf[32];
        std::snprintf(buf, sizeof(buf), "%.17g", c.value_num);
        out += buf;
    } else if (!c.value_str.empty()) {
        out += ",\"value\":\"";
        json_escape(c.value_str, out);
        out += '"';
    }
    if (c.precision != 0) {
        out += ",\"precision\":";
        out += std::to_string(c.precision);
    }
    if (!c.placeholder.empty()) {
        out += ",\"placeholder\":\"";
        json_escape(c.placeholder, out);
        out += '"';
    }
    if (!c.unit.empty()) {
        out += ",\"unit\":\"";
        json_escape(c.unit, out);
        out += '"';
    }
    if (!c.delta.empty()) {
        out += ",\"delta\":\"";
        json_escape(c.delta, out);
        out += '"';
    }
    if (!c.src.empty()) {
        out += ",\"src\":\"";
        json_escape(c.src, out);
        out += '"';
    }
    if (!c.alt.empty()) {
        out += ",\"alt\":\"";
        json_escape(c.alt, out);
        out += '"';
    }
    if (c.width != 0) {
        out += ",\"width\":";
        out += std::to_string(c.width);
    }
    if (c.height != 0) {
        out += ",\"height\":";
        out += std::to_string(c.height);
    }

    out += '}';
    return out;
}

bool Plugin::read(Message& out) {
    std::string line;
    if (!std::getline(std::cin, line)) return false;

    // Strip trailing CR (Windows-friendliness even though notbbg
    // doesn't deploy there today).
    while (!line.empty() && (line.back() == '\r' || line.back() == '\n')) line.pop_back();

    out.topic.clear();
    out.payload_raw.clear();

    // Inbound shape: {"Topic":"…","Payload":<json-value>}
    // We want to extract Topic (string) + Payload (the raw JSON
    // text after `"Payload":`). Hand-rolled because we want zero
    // deps and the shape is fixed.
    auto find_field = [&line](const char* key) -> std::size_t {
        std::string needle = std::string("\"") + key + "\":";
        return line.find(needle);
    };

    // Topic (string).
    if (auto pos = find_field("Topic"); pos != std::string::npos) {
        auto q1 = line.find('"', pos + 8);  // after `"Topic":`
        if (q1 != std::string::npos) {
            // Find the closing quote (no escape support — topics
            // don't contain quotes by convention).
            auto q2 = line.find('"', q1 + 1);
            if (q2 != std::string::npos) {
                out.topic = line.substr(q1 + 1, q2 - q1 - 1);
            }
        }
    }

    // Payload (raw JSON value — could be object, array, string,
    // number, bool, null). Find the value bounds by walking
    // brackets. This handles the common nested-object case.
    if (auto pos = find_field("Payload"); pos != std::string::npos) {
        std::size_t i = pos + std::strlen("\"Payload\":");
        while (i < line.size() && (line[i] == ' ' || line[i] == '\t')) ++i;
        if (i >= line.size()) return true;
        char c = line[i];
        if (c == '{' || c == '[') {
            char open = c;
            char close = (c == '{') ? '}' : ']';
            int depth = 0;
            bool in_str = false;
            bool escape = false;
            std::size_t end = i;
            for (; end < line.size(); ++end) {
                char ch = line[end];
                if (in_str) {
                    if (escape) { escape = false; }
                    else if (ch == '\\') { escape = true; }
                    else if (ch == '"') { in_str = false; }
                    continue;
                }
                if (ch == '"') { in_str = true; continue; }
                if (ch == open) { ++depth; }
                else if (ch == close) {
                    if (--depth == 0) { ++end; break; }
                }
            }
            out.payload_raw = line.substr(i, end - i);
        } else if (c == '"') {
            std::size_t end = i + 1;
            bool escape = false;
            for (; end < line.size(); ++end) {
                char ch = line[end];
                if (escape) { escape = false; continue; }
                if (ch == '\\') { escape = true; continue; }
                if (ch == '"') { ++end; break; }
            }
            out.payload_raw = line.substr(i, end - i);
        } else {
            // Number / bool / null — read until comma or '}'.
            std::size_t end = i;
            while (end < line.size() && line[end] != ',' && line[end] != '}') ++end;
            out.payload_raw = line.substr(i, end - i);
        }
    }

    return true;
}

void Plugin::update_cell_grid(const std::string& screen_id,
                              const std::vector<Cell>& cells,
                              bool full_replace) {
    std::string payload = "{\"screen_id\":\"";
    json_escape(screen_id, payload);
    payload += "\",\"cells\":[";
    for (std::size_t i = 0; i < cells.size(); ++i) {
        if (i > 0) payload += ',';
        payload += cell_to_json(cells[i]);
    }
    payload += "]";
    if (full_replace) payload += ",\"full_replace\":true";
    payload += ",\"version\":\"cellgrid/v1\"}";

    publish_raw(screen_topic_, payload);
}

void Plugin::publish_raw(const std::string& topic, const std::string& payload_json_value) {
    std::string out = "{\"Topic\":\"";
    json_escape(topic, out);
    out += "\",\"Payload\":";
    out += payload_json_value;
    out += "}\n";
    std::cout << out;
    std::cout.flush();
}

}  // namespace notbbg
