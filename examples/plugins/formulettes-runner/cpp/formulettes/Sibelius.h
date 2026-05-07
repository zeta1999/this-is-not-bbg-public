// Sibelius.h — minimal vendored shim.
//
// The upstream Sibelius/formulettes library carries a full
// 800+ line "common header" with picojson glue, Windows time
// helpers, executable-path utilities, threading helpers, etc.
// None of that is needed for the Black-Scholes + barrier pricing
// the formulettes-runner plugin uses, so the OSS vendor mirror
// strips it down to the two symbols BlackScholes.h /
// BarrierAnalytics.h actually depend on.
//
// If you're syncing changes back to upstream, do NOT copy this
// file — it's the OSS-cut shim only.
#pragma once

#include <stdexcept>
#include <string>
#include <sstream>

namespace Sibelius {
    // sqrt(2*pi) — used by the gaussian density n(x) in BlackScholes.h
    // and the implied-vol Hagan SABR helper.
    constexpr double sqrt_two_pi = 2.5066282746310002;

    // Domain-error escape used by BarrierAnalytics on a bad enum
    // tag. Upstream throws a richer Sibelius::SibeliusError; the
    // shim throws std::runtime_error so the OSS plugin doesn't drag
    // in the upstream error class.
    inline void Error(const std::string& msg) {
        throw std::runtime_error(msg);
    }
}

// SibeliusAssert — guards barrier-formula preconditions (S>H or S<H
// depending on the type). Upstream uses a richer assertion macro
// that hooks into the Sibelius error chain; the OSS shim throws so
// the failure surfaces as a non-zero exit out of bs-cli rather than
// a silent NaN.
#define SibeliusAssert(cond) \
    do { \
        if (!(cond)) { \
            std::ostringstream _oss; \
            _oss << "SibeliusAssert(" #cond ") @ " << __FILE__ << ":" << __LINE__; \
            ::Sibelius::Error(_oss.str()); \
        } \
    } while (0)
