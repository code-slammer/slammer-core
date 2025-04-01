import os
import uuid
import pwd

def test():
    return str(uuid.uuid4())

if __name__ == '__main__':
    print("here is a random UUID: ", test())
    print("here is some environment info: ", os.environ)
    print("this is the current working directory: ", os.getcwd())
    print("this is the current user: ", pwd.getpwuid(os.getuid())[0])