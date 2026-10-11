#include "allinclude.h"  //DO NOT edit this line

Status GetTop_L(LStack S, ElemType &e)
{    // Add your code here
    // 无头结点链栈: 栈顶指针为 NULL 即空栈 (同 DC02PE53 StackEmpty_L 惯例)
    if (S == NULL) {
        return ERROR;
    }
    e = S->data;
    return OK;
}
