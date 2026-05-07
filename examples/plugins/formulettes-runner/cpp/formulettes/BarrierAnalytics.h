#pragma once
#include "BlackScholes.h"

namespace Sibelius {
    enum BarrierType {
        BarrierNone = 0,
        DOC,
        UOC,
        DOP,
        UOP,
        DIC,
        UIC,
        DIP,
        UIP 
    };

    inline double _phi(double x) { return phi(x); }

    // Barrier
    struct BarrierCompute {
        double eta;
        double phi;

        double b;
        double r;
        double T;
        double S;
        double X; // strike
        double sigma;
        double H; // barrier
        double K; // rebate

        double A;
        double B;
        double C;
        double D;
        double E;
        double F;

        void compute() {
            double mu = (b - sigma*sigma/2.0) / (sigma*sigma);
            double lambda = std::sqrt(mu*mu + 2.0*r / (sigma*sigma));
            double z = std::log(H/S) / (sigma*std::sqrt(T)) + lambda*sigma*std::sqrt(T);
            double x1 = std::log(S/X)/(sigma*std::sqrt(T)) + (1.0+mu)*sigma*std::sqrt(T);
            double x2 = std::log(S/H)/(sigma*std::sqrt(T)) + (1.0+mu)*sigma*std::sqrt(T);
            double y1 = std::log(H*H/(S*X))/(sigma*std::sqrt(T)) + (1.0+mu)*sigma*std::sqrt(T);
            double y2 = std::log(H/S)/(sigma*std::sqrt(T)) + (1.0+mu)*sigma*std::sqrt(T);

            A = phi * S * std::exp((b-r)*T)*_phi(phi*x1) - phi*X*std::exp(-r*T)*_phi(phi*x1 - phi*sigma*std::sqrt(T));
            B = phi * S * std::exp((b-r)*T)*_phi(phi*x2) - phi*X*std::exp(-r*T)*_phi(phi*x2 - phi*sigma*std::sqrt(T));
            C = phi * S * std::exp((b-r)*T)*std::pow(H/S,2.0*(mu+1.0))*_phi(eta*y1) - phi*X*std::exp(-r*T)*std::pow(H/S, 2.0*mu)*_phi(eta*y1 - eta*sigma*std::sqrt(T));
            D = phi * S * std::exp((b-r)*T)*std::pow(H/S,2.0*(mu+1.0))*_phi(eta*y2) - phi*X*std::exp(-r*T)*std::pow(H/S, 2.0*mu)*_phi(eta*y2 - eta*sigma*std::sqrt(T));
            E = K*std::exp(-r * T)*(_phi(eta*x2 - eta*sigma*std::sqrt(T)) - std::pow(H/S, 2.0*mu)*_phi(eta*y2 - eta*sigma*std::sqrt(T)));
            F = K*(std::pow(H/S, mu+lambda)*_phi(eta*z) + std::pow(H/S, mu-lambda)*_phi(eta*z - 2.0*eta*lambda*sigma*std::sqrt(T)));
        }
    };

    inline double BarrierOption(BarrierType bt, double S, double X, double H, double K, double b, double r, double T, double sigma) {
        BarrierCompute bc;
        bc.S = S;
        bc.X = X;
        bc.H = H;
        bc.K = K;
        bc.b = b;
        bc.r = r;
        bc.T = T;
        bc.sigma = sigma;

        switch(bt) {
            case DIC:
            {
                SibeliusAssert(S > H);
                // ... else K at expiration
                if (X > H) {
                    bc.eta = 1;
                    bc.phi = 1;
                    bc.compute();

                    double rv = bc.C+bc.E;
                    return rv;
                } else {
                    bc.eta = 1;
                    bc.phi = 1;
                    bc.compute();

                    double rv = bc.A - bc.B + bc.D + bc.E;
                    return rv;
                }
            }
            case UIC:
            {
                SibeliusAssert(S < H);
                if (X>H) {
                    bc.eta = -1;
                    bc.phi = 1;
                    bc.compute();

                    double rv = bc.A + bc.E;
                    return rv;
                } else {
                    bc.eta = -1;
                    bc.phi = 1;
                    bc.compute();

                    double rv = bc.B - bc.C + bc.D + bc.E;
                    return rv;
                }
            }
            case DIP:
            {
                SibeliusAssert(S > H);
                if (X>H) {
                    bc.eta = 1;
                    bc.phi = -1;
                    bc.compute();

                    double rv = bc.B - bc.C + bc.D + bc.E;
                    return rv;
                } else {
                    bc.eta = 1;
                    bc.phi = -1;
                    bc.compute();

                    double rv = bc.A + bc.E;
                    return rv;
                }
            }
            case UIP:
            {
                SibeliusAssert(S<H);
                if (X>H) {
                    bc.eta = -1;
                    bc.phi = -1;
                    bc.compute();

                    double rv = bc.A - bc.B + bc.D + bc.E;
                    return rv;
                } else {
                    bc.eta = -1;
                    bc.phi = -1;
                    bc.compute();

                    double rv = bc.C + bc.E;
                    return rv;
                }
            }
            case DOC:
            {
                SibeliusAssert(S > H);
                if (X > H) {
                    bc.eta = 1;
                    bc.phi = 1;
                    bc.compute();

                    double rv = bc.A - bc.C + bc.F;
                    return rv;
                } else {
                    bc.eta = 1;
                    bc.phi = 1;
                    bc.compute();

                    double rv = bc.B - bc.D + bc.F;
                    return rv;
                }
            }
            case UOC:
            {
                SibeliusAssert(S < H);
                if (X > H) {
                    bc.eta = -1;
                    bc.phi = 1;
                    bc.compute();

                    double rv = bc.F;
                    return rv;
                } else {
                    bc.eta = -1;
                    bc.phi = 1;
                    bc.compute();

                    double rv = bc.A - bc.B + bc.C - bc.D + bc.F;
                    return rv;
                }
            }
            case DOP:
            {
                SibeliusAssert(S > H);
                if (X > H) {
                    bc.eta = 1;
                    bc.phi = -1;
                    bc.compute();

                    double rv = bc.A - bc.B + bc.C - bc.D + bc.F;
                    return rv;
                } else {
                    bc.eta = 1;
                    bc.phi = -1;
                    bc.compute();

                    double rv = bc.F;
                    return rv;
                }
            }
            case UOP:
            {
                SibeliusAssert(S<H);
                if (X > H) {
                    bc.eta = -1;
                    bc.phi = -1;
                    bc.compute();

                    double rv = bc.B - bc.D + bc.F;
                    return rv;
                } else {
                    bc.eta = -1;
                    bc.phi = -1;
                    bc.compute(); 
                    
                    double rv = bc.A - bc.C + bc.F;
                    return rv;
                }
            }
            default:
                Sibelius::Error("bad barrier type");
                return 0;
        }
    }

    // Broadie Glasserman approx

}