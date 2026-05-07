// Conformance tests for libs/pluginsdk-cpp. No external test
// framework — keeps the build dep-free. Failures abort with a
// short message; CMake reports per-case via the single binary.

#include "notbbg/pluginsdk.hpp"

#include <cassert>
#include <cstdlib>
#include <iostream>
#include <string>

#define CHECK(cond, msg)                                                 \
    do {                                                                 \
        if (!(cond)) {                                                   \
            std::cerr << "FAIL: " << msg << " at " << __FILE__ << ":"    \
                      << __LINE__ << "\n";                               \
            std::exit(1);                                                \
        }                                                                \
    } while (false)

static bool contains(const std::string& haystack, const std::string& needle) {
    return haystack.find(needle) != std::string::npos;
}

static void test_text_cell_minimal() {
    auto c = notbbg::text_cell(0, 0, "hi");
    auto j = notbbg::cell_to_json(c);
    CHECK(contains(j, "\"type\":\"text\""), j);
    CHECK(contains(j, "\"text\":\"hi\""), j);
    CHECK(contains(j, "\"address\":{\"row\":0,\"col\":0}"), j);
    // Empty fields must be absent.
    CHECK(!contains(j, "placeholder"), j);
    CHECK(!contains(j, "src"), j);
    CHECK(!contains(j, "style"), j);
}

static void test_header_cell_has_bold_style() {
    auto c = notbbg::header_cell(0, 0, "TITLE");
    auto j = notbbg::cell_to_json(c);
    CHECK(contains(j, "\"style\":{\"bold\":true}"), j);
    CHECK(contains(j, "\"text\":\"TITLE\""), j);
}

static void test_number_cell_serialises_value_unit_precision() {
    auto c = notbbg::number_cell(2, 1, "PnL", -123.45, 2, "$");
    auto j = notbbg::cell_to_json(c);
    CHECK(contains(j, "\"type\":\"number\""), j);
    CHECK(contains(j, "\"value\":"), j);
    CHECK(contains(j, "\"precision\":2"), j);
    CHECK(contains(j, "\"unit\":\"$\""), j);
    // Number must contain "-123.45" — checking just "-123" guards
    // against printf locale weirdness with the comma.
    CHECK(contains(j, "-123"), j);
}

static void test_decimal_input_cell_distinct_type() {
    auto c = notbbg::decimal_input_cell(0, 0, "vol", 0.2, 4);
    auto j = notbbg::cell_to_json(c);
    CHECK(contains(j, "\"type\":\"input_decimal\""), j);
    CHECK(contains(j, "\"precision\":4"), j);
}

static void test_json_escape_handles_special_chars() {
    std::string out;
    notbbg::json_escape("a\"b\\c\nd", out);
    CHECK(out == "a\\\"b\\\\c\\nd", out);
}

int main() {
    test_text_cell_minimal();
    test_header_cell_has_bold_style();
    test_number_cell_serialises_value_unit_precision();
    test_decimal_input_cell_distinct_type();
    test_json_escape_handles_special_chars();
    std::cout << "ok — pluginsdk-cpp tests\n";
    return 0;
}
