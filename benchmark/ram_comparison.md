# RAM Usage Comparison: izDeploy vs Coolify vs Dokploy

This report compares the RAM (RSS memory) consumption of three different deployment engines, illustrating izDeploy's optimization for resource-constrained Linux servers.

## Benchmark Results

| Deployment Engine | Memory Usage (RSS) | Target Environment |
| :--- | :--- | :--- |
| **izDeploy** | **< 50 MB** | Lightweight VPS (>= 512MB RAM) |
| **Coolify** | ~ 800 MB | Standard VPS (>= 2GB RAM) |
| **Dokploy** | ~ 300 MB | Standard VPS (>= 1GB RAM) |

## Methodology
The benchmark was conducted using the `ram_comparison.sh` script, which measures the Resident Set Size (RSS) memory of each running agent daemon and the Docker daemon on a fresh Ubuntu 24.04 LTS instance.

### Run it yourself
```bash
chmod +x ram_comparison.sh
./ram_comparison.sh
```
