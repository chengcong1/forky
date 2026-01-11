# Polars
```bash
pip install polars fastexcel
```
```python
import polars as pl
```
# 相关链接
- https://datashare-duo.github.io/datashare

## 合并多个DataFrame
```python
file_path = ["./data/data1.xlsx", "./data/data2.xlsx", "./data/data3.xlsx"]
all_data = []
for file in file_path:
    df = pl.read_excel(file) # 使用 Polars 读取 Excel 文件
    all_data.append(df)
```

## 合并所有数据
```python
df = pl.concat(all_data, how="vertical")  # default is 'vertical' strategy
```
## 根据指定列计算最大值
```python
max_values = (
    df
    .group_by(["A", "B", "C"])
    .agg(pl.col("DD").max().alias("DDD"))
    .sort(["A", "B", "C"])
)
```
## 保存到Excel
```python
df.write_excel('DF.xlsx')
```
## 计算平均值
```python
newdf = (
    df
    .group_by(["A", "B"])
    .agg(pl.col("DD").mean().alias("DD"))
    .sort(["A", "B"])
)
```
# 计算多列的最大值并创建新列
```python
newdf = df.with_columns(
    pl.max_horizontal(["A", "B", "C"])
    .alias("P")
)
```


