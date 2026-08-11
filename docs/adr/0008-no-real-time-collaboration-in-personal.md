# Personal 模式暂不实现实时协作

Personal 模式支持多个客户端通过 Revision、Change Cursor、手动刷新和低频轮询共享同一个 Server，但不引入实时协作。这样可以先验证数据模型和冲突处理；Team 模式再加入实时推送、权限和后台协调组件。

