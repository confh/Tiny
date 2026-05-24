import time

def is_prime(n):
    if n <= 1: return False
    # Equivalent logic
    i = 2
    while i * i <= n:
        if n % i == 0: return False
        i += 1
    return True

start = time.perf_counter()
count = 0
for i in range(2, 50000):
    if is_prime(i):
        count += 1
end = time.perf_counter()

print(f"Primes found: {count}")
print(f"Python Pure Logic Elapsed: {int((end - start) * 1000)}ms")