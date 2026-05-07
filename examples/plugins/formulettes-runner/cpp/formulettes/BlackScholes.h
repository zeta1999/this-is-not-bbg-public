#pragma once
#include "Sibelius.h"

namespace Sibelius {
    // Normal CDF and Black-Scholes formulae
    inline double
    phi(double x)
        {
        static const double RT2PI = sqrt(4.0*acos(0.0));

        static const double SPLIT = 7.07106781186547;

        static const double N0 = 220.206867912376;
        static const double N1 = 221.213596169931;
        static const double N2 = 112.079291497871;
        static const double N3 = 33.912866078383;
        static const double N4 = 6.37396220353165;
        static const double N5 = 0.700383064443688;
        static const double N6 = 3.52624965998911e-02;
        static const double M0 = 440.413735824752;
        static const double M1 = 793.826512519948;
        static const double M2 = 637.333633378831;
        static const double M3 = 296.564248779674;
        static const double M4 = 86.7807322029461;
        static const double M5 = 16.064177579207;
        static const double M6 = 1.75566716318264;
        static const double M7 = 8.83883476483184e-02;

        const double z = fabs(x);
        double c = 0.0;

        if(z<=37.0)
        {
            const double e = exp(-z*z/2.0);
            if(z<SPLIT)
            {
            const double n = (((((N6*z + N5)*z + N4)*z + N3)*z + N2)*z + N1)*z + N0;
            const double d = ((((((M7*z + M6)*z + M5)*z + M4)*z + M3)*z + M2)*z + M1)*z + M0;
            c = e*n/d;
            }
            else
            {
            const double f = z + 1.0/(z + 2.0/(z + 3.0/(z + 4.0/(z + 13.0/20.0))));
            c = e/(RT2PI*f);
            }
        }
        return x<=0.0 ? c : 1-c;
    }

    // alternative for test only
    inline double phi2(double x)
    {
        // constants
        double a1 =  0.254829592;
        double a2 = -0.284496736;
        double a3 =  1.421413741;
        double a4 = -1.453152027;
        double a5 =  1.061405429;
        double p  =  0.3275911;

        // Save the sign of x
        int sign = 1;
        if (x < 0)
            sign = -1;
        x = fabs(x)/sqrt(2.0);

        // A&S formula 7.1.26
        double t = 1.0/(1.0 + p*x);
        double y = 1.0 - (((((a5*t + a4)*t) + a3)*t + a2)*t + a1)*t*exp(-x*x);

        return 0.5*(1.0 + sign*y);
    }

    // 1/\sqrt{2 \pi}e^{-x^2/2}
    inline double n(double x) {
        return std::exp(-x*x/2)/sqrt_two_pi;
    }

    // b = r-y
    // test: put 75 70 0.5 0.1 0.05 0.35 = 4.0870
    inline double BS(bool isCall, double S, double X, double T, double r, double b, double v) {
        auto d1 = (std::log(S/X) + (b + v*v/2.0)*T) / (v*std::sqrt(T));
        auto d2 = d1 - v*std::sqrt(T);
        if (isCall) {
            return S*std::exp((b-r)*T)*phi(d1) - X*std::exp(-r*T)*phi(d2);
        } else {
            return X*std::exp(-r*T)*phi(-d2) - S*std::exp((b-r)*T)*phi(-d1);
        }
    }

    inline double BSVega(double S, double X, double T, double r, double b, double v) {
        auto d1 = (std::log(S/X) + (b + v*v/2.0)*T) / (v*std::sqrt(T));
        return S*std::exp((b-r)*T)*n(d1)*std::sqrt(T);
    }

    // Upstream Sibelius/formulettes also defines BSEscrowed,
    // BSImplied{Bissection,NewToRaphson,Rational}, BSImplied,
    // BSEscrowedImplied. They're stripped from the OSS shim because
    // bs.cpp only calls BS + BSVega; if you need implied-vol here,
    // re-vendor the missing entry points from upstream.
}
