#include <stdint.h>

typedef unsigned long size_t;

void* memcpy(void* dest, const void* src, size_t n) {
    uint8_t* d = (uint8_t*)dest;
    const uint8_t* s = (const uint8_t*)src;
    while (n--) *d++ = *s++;
    return dest;
}

void* memset(void* s, int c, size_t n) {
    uint8_t* p = (uint8_t*)s;
    while (n--) *p++ = (uint8_t)c;
    return s;
}

#define CH(x, y, z) (((x) & (y)) ^ (~(x) & (z)))
#define MAJ(x, y, z) (((x) & (y)) ^ ((x) & (z)) ^ ((y) & (z)))
#define ROTR(x, n) (((x) >> (n)) | ((x) << (32 - (n))))
#define SIGMA0(x) (ROTR(x, 2) ^ ROTR(x, 13) ^ ROTR(x, 22))
#define SIGMA1(x) (ROTR(x, 6) ^ ROTR(x, 11) ^ ROTR(x, 25))
#define sigma0(x) (ROTR(x, 7) ^ ROTR(x, 18) ^ ((x) >> 3))
#define sigma1(x) (ROTR(x, 17) ^ ROTR(x, 19) ^ ((x) >> 10))

static const uint32_t K[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
};

void sha256_transform(uint32_t state[8], const uint8_t data[64]) {
    uint32_t a, b, c, d, e, f, g, h, t1, t2, m[64];
    int i, j;
    for (i = 0, j = 0; i < 16; ++i, j += 4)
        m[i] = (data[j] << 24) | (data[j + 1] << 16) | (data[j + 2] << 8) | data[j + 3];
    for (; i < 64; ++i) m[i] = sigma1(m[i - 2]) + m[i - 7] + sigma0(m[i - 15]) + m[i - 16];
    a = state[0]; b = state[1]; c = state[2]; d = state[3];
    e = state[4]; f = state[5]; g = state[6]; h = state[7];
    for (i = 0; i < 64; ++i) {
        t1 = h + SIGMA1(e) + CH(e, f, g) + K[i] + m[i];
        t2 = SIGMA0(a) + MAJ(a, b, c);
        h = g; g = f; f = e; e = d + t1;
        d = c; c = b; b = a; a = t1 + t2;
    }
    state[0] += a; state[1] += b; state[2] += c; state[3] += d;
    state[4] += e; state[5] += f; state[6] += g; state[7] += h;
}

uint8_t buffer[256];

__attribute__((visibility("default"))) uint8_t* get_buffer() { return buffer; }

static int leading_zero_match(uint32_t state[8], int difficulty) {
    if (difficulty <= 0) return 1;
    if (difficulty <= 32) return (state[0] >> (32 - difficulty)) == 0;
    uint8_t hash[32];
    int byteCount = difficulty / 8, bitCount = difficulty % 8;
    for (int i = 0; i < 8; i++) {
        hash[i * 4] = (state[i] >> 24) & 0xff;
        hash[i * 4 + 1] = (state[i] >> 16) & 0xff;
        hash[i * 4 + 2] = (state[i] >> 8) & 0xff;
        hash[i * 4 + 3] = state[i] & 0xff;
    }
    for (int i = 0; i < byteCount; i++) if (hash[i] != 0) return 0;
    return bitCount == 0 || (hash[byteCount] & (0xFF << (8 - bitCount))) == 0;
}

__attribute__((visibility("default")))
int solve_pow(int data_len, int difficulty,
              uint32_t start_nonce_lo, uint32_t start_nonce_hi,
              uint32_t step_lo, uint32_t step_hi,
              int max_iterations) {
    uint64_t start_nonce = ((uint64_t)start_nonce_hi << 32) | start_nonce_lo;
    uint64_t step = ((uint64_t)step_hi << 32) | step_lo;
    if (data_len < 0 || data_len > 120 || difficulty > 255 || max_iterations <= 0 || step == 0) return -1;
    uint8_t block[128];
    memset(block, 0, 128);
    memcpy(block, buffer, data_len);
    for (int iter = 0; iter < max_iterations; ++iter) {
        uint64_t nonce = start_nonce + (uint64_t)iter * step;
        int nlen = 0, si = 0;
        char s[24];
        if (nonce == 0) s[si++] = '0';
        while (nonce) { s[si++] = (nonce % 10) + '0'; nonce /= 10; }
        while (si--) block[data_len + nlen++] = s[si];
        int total_len = data_len + nlen;
        if (total_len > 119) return -1;
        memset(block + total_len, 0, 128 - total_len);
        block[total_len] = 0x80;
        int block_len = ((total_len + 8) / 64 + 1) * 64;
        uint64_t bit_len = (uint64_t)total_len * 8;
        for (int i = 0; i < 8; i++) block[block_len - 1 - i] = (bit_len >> (i * 8)) & 0xff;
        uint32_t state[8] = {0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19};
        for (int i = 0; i < block_len; i += 64) sha256_transform(state, &block[i]);
        if (leading_zero_match(state, difficulty)) {
            for (int i = 0; i < nlen; i++) buffer[i] = block[data_len + i];
            buffer[nlen] = 0;
            return iter + 1;
        }
    }
    return 0;
}
