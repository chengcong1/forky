# tkinter
```python
import tkinter as tk
from tkinter import filedialog
```
## 文件选择对话框 · 选择多个指定类型的文件 askopenfilenames
```python
paths = filedialog.askopenfilenames(title="选择EXCEL文件", filetypes=[("Excel", " *.xlsx"), ("All Files", "*")])
```
## 文件选择对话框 · 选择多个指定类型的文件 askopenfilename
```python
path = filedialog.askopenfilename(title="选择EXCEL文件", filetypes=[("Excel", " *.xlsx"), ("All Files", "*")])
```










