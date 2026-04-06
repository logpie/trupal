from main import add, divide, Calculator

def test_add():
    assert add(2, 3) == 5

def test_divide():
    assert divide(10, 2) == 5.0

def test_calculator():
    calc = Calculator()
    assert calc.compute("add", 1, 2) == 3
