// bs.cpp — multi-formula CLI wrapping the Sibelius::formulettes
// header-only library. Two subcommands today:
//
//   bs-cli <spot> <strike> <vol> <rate> <time> <call|put> [b]
//       Plain Black-Scholes (positional, unchanged for back-compat).
//
//   bs-cli barrier <type> <S> <X> <H> <K> <b> <r> <T> <sigma>
//       Barrier option. <type> ∈ {DOC,UOC,DOP,UOP,DIC,UIC,DIP,UIP}.
//       Outputs price only (greeks via finite differences could be
//       added later — barrier formulas already capture the discontinuous
//       knock-out structure analytically).
//
// Self-contained — pure stdlib + the vendored formulettes headers.
// Vendored from Sibelius/formulettes — see cpp/formulettes/. Keeps
// the OSS plugin buildable from a fresh clone with no external dep.

#include "formulettes/BlackScholes.h"
#include "formulettes/BarrierAnalytics.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

using Sibelius::BS;
using Sibelius::BSVega;
using Sibelius::BarrierOption;
using Sibelius::BarrierType;

static double parse_d(const char* s) {
    return std::strtod(s, nullptr);
}

static int parseBarrierType(const char* s, BarrierType& out) {
    struct map { const char* name; BarrierType t; };
    static const map table[] = {
        {"DOC", Sibelius::DOC}, {"UOC", Sibelius::UOC},
        {"DOP", Sibelius::DOP}, {"UOP", Sibelius::UOP},
        {"DIC", Sibelius::DIC}, {"UIC", Sibelius::UIC},
        {"DIP", Sibelius::DIP}, {"UIP", Sibelius::UIP},
    };
    for (auto& m : table) {
        if (std::strcmp(s, m.name) == 0) { out = m.t; return 0; }
    }
    return -1;
}

static int runBarrier(int argc, char** argv) {
    // argv[0] is "barrier"; expect 9 more args.
    if (argc < 10) {
        std::fprintf(stderr,
            "usage: bs-cli barrier <type> <S> <X> <H> <K> <b> <r> <T> <sigma>\n"
            "  type ∈ {DOC,UOC,DOP,UOP,DIC,UIC,DIP,UIP}\n");
        return 2;
    }
    BarrierType bt;
    if (parseBarrierType(argv[1], bt) != 0) {
        std::fprintf(stderr, "unknown barrier type: %s\n", argv[1]);
        return 1;
    }
    double S = parse_d(argv[2]);
    double X = parse_d(argv[3]);
    double H = parse_d(argv[4]);
    double K = parse_d(argv[5]);
    double b = parse_d(argv[6]);
    double r = parse_d(argv[7]);
    double T = parse_d(argv[8]);
    double sigma = parse_d(argv[9]);
    if (T <= 0 || sigma <= 0 || S <= 0 || X <= 0 || H <= 0) {
        std::fprintf(stderr, "bad inputs: T,sigma,S,X,H must all be > 0\n");
        return 1;
    }
    double price = BarrierOption(bt, S, X, H, K, b, r, T, sigma);

    // Greeks via central finite differences. Sibelius's
    // BarrierOption is closed-form per Reiner-Rubinstein so each
    // bump is O(1); we run 8 extra evaluations to populate the
    // delta/gamma/vega/theta/rho display the BS path already shows.
    const double hS    = std::max(1e-4, S * 1e-4);
    const double hSig  = std::max(1e-6, sigma * 1e-4);
    const double hT    = std::max(1.0 / 365.0 * 0.01, T * 1e-4);
    const double hR    = 1e-5;
    double pSu = BarrierOption(bt, S + hS, X, H, K, b, r, T, sigma);
    double pSd = BarrierOption(bt, S - hS, X, H, K, b, r, T, sigma);
    double pVu = BarrierOption(bt, S, X, H, K, b, r, T, sigma + hSig);
    double pTd = (T > hT) ? BarrierOption(bt, S, X, H, K, b, r, T - hT, sigma) : price;
    double pRu = BarrierOption(bt, S, X, H, K, b + hR, r + hR, T, sigma);
    double delta = (pSu - pSd) / (2 * hS);
    double gamma = (pSu - 2 * price + pSd) / (hS * hS);
    double vega  = (pVu - price) / hSig;   // per unit-vol (1.00 = 100 vol pts)
    double theta = (pTd - price) / hT;     // per year
    double rho   = (pRu - price) / hR;

    std::printf("{\"price\":%.10g,\"delta\":%.10g,\"gamma\":%.10g,"
                "\"vega\":%.10g,\"theta\":%.10g,\"rho\":%.10g,"
                "\"type\":\"%s\"}\n",
                price, delta, gamma, vega, theta, rho, argv[1]);
    return 0;
}

static int runBS(int argc, char** argv) {
    // argv layout: [bs?] <spot> <strike> <vol> <rate> <time> <call|put> [b]
    // The "bs" subcommand prefix is optional for back-compat with the
    // tests + the original positional invocation.
    int off = 0;
    if (argc >= 1 && std::strcmp(argv[0], "bs") == 0) {
        off = 1;
    }
    if (argc - off < 6) {
        std::fprintf(stderr,
            "usage: bs-cli [bs] <spot> <strike> <vol> <rate> <time> <call|put> [b]\n");
        return 2;
    }
    double S = parse_d(argv[off + 0]);
    double X = parse_d(argv[off + 1]);
    double v = parse_d(argv[off + 2]);
    double r = parse_d(argv[off + 3]);
    double T = parse_d(argv[off + 4]);
    bool isCall = (std::strcmp(argv[off + 5], "call") == 0
                || std::strcmp(argv[off + 5], "Call") == 0
                || std::strcmp(argv[off + 5], "C") == 0);
    double b = (argc - off > 6) ? parse_d(argv[off + 6]) : r;

    if (T <= 0 || v <= 0 || S <= 0 || X <= 0) {
        std::fprintf(stderr, "bad inputs: T,v,S,X must all be > 0\n");
        return 1;
    }

    double price = BS(isCall, S, X, T, r, b, v);
    double vega = BSVega(S, X, T, r, b, v);

    const double hS = std::max(1e-4, S * 1e-4);
    double pUp   = BS(isCall, S + hS, X, T, r, b, v);
    double pDn   = BS(isCall, S - hS, X, T, r, b, v);
    double delta = (pUp - pDn) / (2 * hS);
    double gamma = (pUp - 2 * price + pDn) / (hS * hS);

    const double dt = std::max(1.0 / 365.0 * 0.01, T * 1e-4);
    double pT    = (T > dt) ? BS(isCall, S, X, T - dt, r, b, v) : price;
    double theta = (pT - price) / dt;

    const double dr = 1e-5;
    double pR    = BS(isCall, S, X, T, r + dr, b + dr, v);
    double rho   = (pR - price) / dr;

    std::printf("{\"price\":%.10g,\"delta\":%.10g,\"gamma\":%.10g,"
                "\"vega\":%.10g,\"theta\":%.10g,\"rho\":%.10g}\n",
                price, delta, gamma, vega, theta, rho);
    return 0;
}

// dispatch routes one parsed argv-style request (already shorn of
// the "bs-cli" program name) to the right handler, mirroring main.
static int dispatch(int argc, char** argv) {
    if (argc >= 1 && std::strcmp(argv[0], "barrier") == 0) {
        return runBarrier(argc, argv);
    }
    return runBS(argc, argv);
}

// runREPL reads newline-delimited requests from stdin, dispatches
// each one, and flushes a JSON response per line. Eliminates the
// ~450 ms macOS exec startup hit that the formulettes plugin used
// to pay on every keystroke. On a bad request the line is
// `{"error":"..."}` so the caller can resync without restarting
// the process.
//
// Tokenization is whitespace-split on the line, matching the
// existing argv format — keeps the protocol trivial and the C++
// side independent of any JSON parser dependency.
static int runREPL() {
    // Disable stdio buffering so the Go side gets each response
    // immediately without a fflush dance. setvbuf with NULL +
    // _IONBF is the standard "no buffering" knob.
    std::setvbuf(stdin, nullptr, _IOLBF, 0);
    std::setvbuf(stdout, nullptr, _IONBF, 0);

    std::string line;
    char buf[4096];
    for (;;) {
        if (std::fgets(buf, sizeof(buf), stdin) == nullptr) {
            return 0; // EOF — clean shutdown
        }
        line = buf;
        // Strip trailing newline / CR.
        while (!line.empty() && (line.back() == '\n' || line.back() == '\r')) {
            line.pop_back();
        }
        if (line.empty()) {
            continue;
        }

        // Split on whitespace into mutable tokens. Storage owned by
        // `tokens`; argv points into it and stays valid for the
        // dispatch call.
        std::vector<std::string> tokens;
        std::string cur;
        for (char c : line) {
            if (c == ' ' || c == '\t') {
                if (!cur.empty()) { tokens.push_back(cur); cur.clear(); }
            } else {
                cur.push_back(c);
            }
        }
        if (!cur.empty()) { tokens.push_back(cur); }
        if (tokens.empty()) { continue; }

        std::vector<char*> argv;
        argv.reserve(tokens.size());
        for (auto& t : tokens) { argv.push_back(t.data()); }

        // dispatch writes the JSON response to stdout (or an error
        // to stderr + non-zero return). Convert any non-zero return
        // into a structured JSON error line so the Go side has
        // exactly one response per request to read.
        std::fflush(stderr);
        int rc = dispatch(static_cast<int>(argv.size()), argv.data());
        if (rc != 0) {
            std::printf("{\"error\":\"bs-cli rc=%d\"}\n", rc);
        }
        std::fflush(stdout);
    }
}

int main(int argc, char** argv) {
    if (argc >= 2 && std::strcmp(argv[1], "--repl") == 0) {
        return runREPL();
    }
    if (argc >= 2 && std::strcmp(argv[1], "barrier") == 0) {
        return runBarrier(argc - 1, argv + 1);
    }
    return runBS(argc - 1, argv + 1);
}
